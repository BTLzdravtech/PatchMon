package migrate

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

func TestDirtyRecoveryMessage(t *testing.T) {
	tests := []struct {
		name        string
		version     int
		wantSQL     string
		notWantSQL  string
		wantDBLabel string
	}{
		{
			name:        "mid-sequence rolls back to the previous version",
			version:     42,
			wantSQL:     "UPDATE schema_migrations SET version = 41, dirty = false;",
			wantDBLabel: `"patchmon_db"`,
		},
		{
			// Forcing version 0 clears the dirty flag but points at a migration
			// that does not exist, which is harder to recover from than staying
			// dirty. Clearing the table is the only way back.
			name:        "first migration clears the table instead of forcing version 0",
			version:     1,
			wantSQL:     "DELETE FROM schema_migrations;",
			notWantSQL:  "version = 0",
			wantDBLabel: `"patchmon_db"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirtyRecoveryMessage("postgres://u:p@host:5432/patchmon_db?sslmode=disable", UpstreamMigrationsTable, tt.version)
			if !strings.Contains(got, tt.wantSQL) {
				t.Errorf("message missing recovery SQL %q:\n%s", tt.wantSQL, got)
			}
			if tt.notWantSQL != "" && strings.Contains(got, tt.notWantSQL) {
				t.Errorf("message must not contain %q:\n%s", tt.notWantSQL, got)
			}
			if !strings.Contains(got, tt.wantDBLabel) {
				t.Errorf("message missing database label %s:\n%s", tt.wantDBLabel, got)
			}
			if strings.Contains(got, "u:p@") {
				t.Errorf("message leaked DSN credentials:\n%s", got)
			}
		})
	}
}

func TestDatabaseName(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"standard dsn", "postgres://user:pass@localhost:5432/patchmon_db?sslmode=disable", "patchmon_db"},
		{"postgresql scheme", "postgresql://user:pass@localhost/patchmon_db", "patchmon_db"},
		{"no database in path", "postgres://user:pass@localhost:5432", "unknown"},
		{"trailing slash only", "postgres://user:pass@localhost:5432/", "unknown"},
		{"unparseable", "://not a url", "unknown"},
		{"empty", "", "unknown"},
		// url.Parse puts the whole keyword/value DSN in Path, so without a
		// scheme check this would print the password to stderr.
		{"keyword value dsn is not echoed", "host=localhost dbname=patchmon_db password=hunter2", "unknown"},
		{"bare name is not echoed", "patchmon_db", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := databaseName(tt.dsn)
			if got != tt.want {
				t.Errorf("databaseName(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
			if strings.Contains(got, "password") || strings.Contains(got, "hunter2") {
				t.Errorf("databaseName(%q) leaked credentials: %q", tt.dsn, got)
			}
		})
	}
}

// ErrDirty is returned by value and reaches Run joined with any unlock error, so
// Run has to unwrap rather than type-assert. This pins the shape Run relies on;
// it would fail if golang-migrate ever made ErrDirty a pointer type.
func TestErrDirtyUnwrapsThroughJoin(t *testing.T) {
	joined := errors.Join(migrate.ErrDirty{Version: 42}, errors.New("unlock failed"))

	var dirty migrate.ErrDirty
	if !errors.As(joined, &dirty) {
		t.Fatal("errors.As failed to unwrap ErrDirty from a joined error")
	}
	if dirty.Version != 42 {
		t.Errorf("Version = %d, want 42", dirty.Version)
	}
}

func TestReportDirtyRecoveryDedup(t *testing.T) {
	const dsnA = "postgres://u:p@host/ctx_a?sslmode=disable"
	const dsnB = "postgres://u:p@host/ctx_b?sslmode=disable"

	t.Cleanup(func() {
		forgetDirtyRecovery(dsnA)
		forgetDirtyRecovery(dsnB)
	})

	var buf strings.Builder
	if !reportDirtyRecovery(&buf, dsnA, dsnA, UpstreamMigrationsTable, 42) {
		t.Fatal("first report for a database must print")
	}
	if !strings.Contains(buf.String(), "ctx_a") {
		t.Errorf("report did not name the database:\n%s", buf.String())
	}

	// A server that retries migrations per request must not reprint per request.
	for i := 0; i < 3; i++ {
		if reportDirtyRecovery(io.Discard, dsnA, dsnA, UpstreamMigrationsTable, 42) {
			t.Fatalf("repeat report %d for the same database and version must be suppressed", i+1)
		}
	}

	// Dedup must not be global: another context is a different database.
	var bufB strings.Builder
	if !reportDirtyRecovery(&bufB, dsnB, dsnB, UpstreamMigrationsTable, 42) {
		t.Error("a different database must still print while another is deduped")
	}
	if strings.Contains(bufB.String(), "ctx_a") {
		t.Errorf("report named the wrong database:\n%s", bufB.String())
	}

	// A different dirty version is new information, so it prints.
	if !reportDirtyRecovery(io.Discard, dsnA, dsnA, UpstreamMigrationsTable, 43) {
		t.Error("a new dirty version for the same database must print")
	}
}

// After a failed retry the database goes dirty at the same version again, which
// is exactly when the operator needs the guidance repeated rather than deduped.
func TestForgetDirtyRecoveryRestoresReporting(t *testing.T) {
	const dsn = "postgres://u:p@host/ctx_retry?sslmode=disable"
	t.Cleanup(func() { forgetDirtyRecovery(dsn) })

	if !reportDirtyRecovery(io.Discard, dsn, dsn, UpstreamMigrationsTable, 42) {
		t.Fatal("first report must print")
	}
	if reportDirtyRecovery(io.Discard, dsn, dsn, UpstreamMigrationsTable, 42) {
		t.Fatal("second report must be suppressed before forgetting")
	}

	forgetDirtyRecovery(dsn)

	if !reportDirtyRecovery(io.Discard, dsn, dsn, UpstreamMigrationsTable, 42) {
		t.Error("report must print again after the database is forgotten")
	}
}

// The postgres driver can write version -1 when a dirty NilVersion is recorded,
// and version 0 has no migration to rewind to either.
func TestDirtyRecoveryMessageNonPositiveVersions(t *testing.T) {
	for _, version := range []int{0, -1} {
		got := dirtyRecoveryMessage("postgres://u:p@host/db", UpstreamMigrationsTable, version)
		if !strings.Contains(got, "DELETE FROM schema_migrations;") {
			t.Errorf("version %d: want the clearing form, got:\n%s", version, got)
		}
		if strings.Contains(got, "SET version =") {
			t.Errorf("version %d: must not tell the operator to force a negative version:\n%s", version, got)
		}
	}
}

func TestEnsureSSLMode(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"adds sslmode when absent", "postgres://localhost/db", "postgres://localhost/db?sslmode=disable"},
		{"appends to existing query", "postgres://localhost/db?connect_timeout=5", "postgres://localhost/db?connect_timeout=5&sslmode=disable"},
		{"leaves explicit sslmode alone", "postgres://localhost/db?sslmode=require", "postgres://localhost/db?sslmode=require"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureSSLMode(tt.dsn); got != tt.want {
				t.Errorf("ensureSSLMode(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestDirtyRecoveryMessageForkTable(t *testing.T) {
	got := dirtyRecoveryMessage("postgres://u:p@host:5432/patchmon_db", ForkMigrationsTable, 2)
	if !strings.Contains(got, "UPDATE schema_migrations_fork SET version = 1, dirty = false;") {
		t.Errorf("fork track recovery must target its own table:\n%s", got)
	}
	got = dirtyRecoveryMessage("postgres://u:p@host:5432/patchmon_db", ForkMigrationsTable, 1)
	if !strings.Contains(got, "DELETE FROM schema_migrations_fork;") {
		t.Errorf("fork track first-migration recovery must clear its own table:\n%s", got)
	}
}

func TestWithMigrationsTable(t *testing.T) {
	tests := []struct {
		name, dsn, table, want string
	}{
		{"upstream table leaves DSN untouched", "postgres://h/db?sslmode=disable", UpstreamMigrationsTable, "postgres://h/db?sslmode=disable"},
		{"fork table appended with &", "postgres://h/db?sslmode=disable", ForkMigrationsTable, "postgres://h/db?sslmode=disable&x-migrations-table=schema_migrations_fork"},
		{"fork table appended with ?", "postgres://h/db", ForkMigrationsTable, "postgres://h/db?x-migrations-table=schema_migrations_fork"},
		{"explicit table respected", "postgres://h/db?x-migrations-table=custom", ForkMigrationsTable, "postgres://h/db?x-migrations-table=custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withMigrationsTable(tt.dsn, tt.table); got != tt.want {
				t.Errorf("withMigrationsTable(%q, %q) = %q, want %q", tt.dsn, tt.table, got, tt.want)
			}
		})
	}
}

// TestForkTrackEmbedded guards the fork directory: it must load as a migrate
// source and never share a version number within itself.
func TestForkTrackEmbedded(t *testing.T) {
	for _, tr := range tracks {
		entries, err := tr.fs.ReadDir(tr.dir)
		if err != nil {
			t.Fatalf("%s track: read embedded dir: %v", tr.name, err)
		}
		seen := map[string]string{}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".sql") {
				continue
			}
			sep := strings.Index(name, "_")
			if sep <= 0 {
				t.Errorf("%s track: %s is not named NNNNNN_description.{up,down}.sql", tr.name, name)
				continue
			}
			ver := name[:sep]
			base := strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql")
			if prev, ok := seen[ver]; ok && prev != base {
				t.Errorf("%s track: version %s used by both %s and %s", tr.name, ver, prev, base)
			}
			seen[ver] = base
		}
		if len(seen) == 0 {
			t.Errorf("%s track: no migrations embedded", tr.name)
		}
	}
}
