// Package migrate runs database migrations at application startup.
// Migrations are embedded in the binary; no separate migrations directory is required.
package migrate

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed migrations_fork/*.sql
var forkMigrationsFS embed.FS

// UpstreamMigrationsTable is golang-migrate's default version table, used by
// the upstream PatchMon migration set.
const UpstreamMigrationsTable = "schema_migrations"

// ForkMigrationsTable tracks the fork-only migrations in migrations_fork/.
//
// golang-migrate keeps a single current version per table rather than a list
// of applied files, so fork migrations cannot share upstream's numbering:
// upstream reusing a number would either refuse to load (duplicate version on
// a fresh database) or be skipped (deployed database already past it). A
// second table gives the fork its own independent sequence.
const ForkMigrationsTable = "schema_migrations_fork"

// track is one embedded migration set together with the table that records
// its version. Tracks run in order; the fork track assumes upstream's schema
// is already in place.
type track struct {
	name  string
	fs    embed.FS
	dir   string
	table string
}

var tracks = []track{
	{name: "upstream", fs: migrationsFS, dir: "migrations", table: UpstreamMigrationsTable},
	{name: "fork", fs: forkMigrationsFS, dir: "migrations_fork", table: ForkMigrationsTable},
}

// Run applies all pending migrations using embedded SQL files: first the
// upstream set, then the fork set. Logs to the provided logger and always
// prints migration status to stdout. Returns an error if any track fails; a
// failing upstream track stops before the fork track runs.
// ErrNoChange is treated as success (already up to date).
func Run(databaseURL string, log *slog.Logger) error {
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required for migrations")
	}

	databaseURL = ensureSSLMode(databaseURL)

	_, _ = fmt.Fprintln(os.Stdout, "[migrate] running migrations from embedded binary")
	log.Info("running migrations", "path", "embedded")

	for _, t := range tracks {
		if err := runTrack(t, databaseURL, log); err != nil {
			return err
		}
	}
	return nil
}

func runTrack(t track, databaseURL string, log *slog.Logger) error {
	m, err := openTrack(t, databaseURL)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	// The dirty-recovery bookkeeping is keyed per database and table so a
	// wedged fork track does not mask or dedupe a later upstream report.
	recoveryKey := databaseURL + "#" + t.table

	upErr := m.Up()
	if upErr != nil && upErr != migrate.ErrNoChange {
		fmt.Fprintf(os.Stderr, "[migrate] %s track failed: %v\n", t.name, upErr)
		var dirty migrate.ErrDirty
		if errors.As(upErr, &dirty) {
			reportDirtyRecovery(os.Stderr, recoveryKey, databaseURL, t.table, dirty.Version)
		} else {
			// A migration actually ran and failed, so the dirty marker was just
			// set fresh. Forget any earlier report for this database, otherwise
			// the dirty error on the next attempt is silently deduped away at
			// exactly the point the operator needs the guidance repeated.
			forgetDirtyRecovery(recoveryKey)
		}
		return fmt.Errorf("migration up (%s track): %w", t.name, upErr)
	}

	forgetDirtyRecovery(recoveryKey)

	if upErr == migrate.ErrNoChange {
		_, _ = fmt.Fprintf(os.Stdout, "[migrate] %s track already up to date\n", t.name)
		log.Info("migrations: already up to date", "track", t.name)
		return nil
	}

	version, _, _ := m.Version()
	_, _ = fmt.Fprintf(os.Stdout, "[migrate] %s track applied successfully (version %d)\n", t.name, version)
	log.Info("migrations applied successfully", "track", t.name, "version", version)
	return nil
}

func openTrack(t track, databaseURL string) (*migrate.Migrate, error) {
	source, err := iofs.New(t.fs, t.dir)
	if err != nil {
		return nil, fmt.Errorf("create embedded migrate source (%s track): %w", t.name, err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, withMigrationsTable(databaseURL, t.table))
	if err != nil {
		return nil, fmt.Errorf("create migrate instance (%s track): %w", t.name, err)
	}
	return m, nil
}

// Open returns a migrate instance for the upstream track, for use by the CLI
// (up/down/force/version). Caller must call m.Close() when done.
func Open(databaseURL string) (*migrate.Migrate, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required for migrations")
	}
	return openTrack(tracks[0], ensureSSLMode(databaseURL))
}

// OpenFork returns a migrate instance for the fork track, for use by the CLI.
// Caller must call m.Close() when done.
func OpenFork(databaseURL string) (*migrate.Migrate, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required for migrations")
	}
	return openTrack(tracks[1], ensureSSLMode(databaseURL))
}

// withMigrationsTable points the postgres driver at a specific version table
// via its x-migrations-table query parameter. The default table is left
// implicit so upstream's DSN handling is untouched for the upstream track.
func withMigrationsTable(databaseURL, table string) string {
	if table == "" || table == UpstreamMigrationsTable {
		return databaseURL
	}
	if strings.Contains(databaseURL, "x-migrations-table=") {
		return databaseURL
	}
	sep := "?"
	if strings.Contains(databaseURL, "?") {
		sep = "&"
	}
	return databaseURL + sep + "x-migrations-table=" + url.QueryEscape(table)
}

// dirtyRecoveryReported tracks the dirty version already reported per database,
// so a server that retries migrations per request does not reprint the block on
// every attempt. Keyed by digest, never the DSN itself.
var (
	dirtyRecoveryMu       sync.Mutex
	dirtyRecoveryReported = map[string]int{}
)

func dirtyRecoveryKey(databaseURL string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(databaseURL)))
}

// reportDirtyRecovery turns golang-migrate's "Dirty database version N. Fix and
// force version." into instructions an operator can act on. The migrate CLI is
// not shipped in the server image, so the recovery is given as SQL. It reports
// whether it wrote anything, which is false when the same database is already
// known to be dirty at the same version.
func reportDirtyRecovery(w io.Writer, recoveryKey, databaseURL, table string, version int) bool {
	key := dirtyRecoveryKey(recoveryKey)

	dirtyRecoveryMu.Lock()
	last, seen := dirtyRecoveryReported[key]
	if seen && last == version {
		dirtyRecoveryMu.Unlock()
		return false
	}
	dirtyRecoveryReported[key] = version
	dirtyRecoveryMu.Unlock()

	_, _ = fmt.Fprint(w, dirtyRecoveryMessage(databaseURL, table, version))
	return true
}

// forgetDirtyRecovery drops a database's recorded dirty version once migrations
// get past it, so a later dirty episode reports again.
func forgetDirtyRecovery(recoveryKey string) {
	dirtyRecoveryMu.Lock()
	delete(dirtyRecoveryReported, dirtyRecoveryKey(recoveryKey))
	dirtyRecoveryMu.Unlock()
}

func dirtyRecoveryMessage(databaseURL, table string, version int) string {
	if table == "" {
		table = UpstreamMigrationsTable
	}
	// Version 1 has no predecessor to roll back to, and forcing version 0 leaves
	// the marker clean but pointing at a migration that does not exist, which is
	// harder to recover from than the dirty state. Clearing the table is what
	// `migrate force -1` does and is the only way back from a dirty first migration.
	recovery := fmt.Sprintf("UPDATE %s SET version = %d, dirty = false;", table, version-1)
	if version <= 1 {
		recovery = fmt.Sprintf("DELETE FROM %s;", table)
	}

	return fmt.Sprintf(`
[migrate] Migration %d did not complete on database %q, so it is marked dirty and
[migrate] no further migrations will run against it until that marker is cleared.
[migrate]
[migrate] The error logged above this block, on the run that first failed, is the
[migrate] cause. Fix that first, then confirm migration %d left nothing behind
[migrate] (it is applied as a single transaction, so a failed run rolls back, but
[migrate] a server killed mid-migration can leave partial changes). Then run this
[migrate] against that database:
[migrate]
[migrate]   %s
[migrate]
[migrate] and restart the server so migration %d is retried. Make sure the server
[migrate] is on a build that fixes the cause before retrying, or it will fail again.
`, version, databaseName(databaseURL), version, recovery, version)
}

// databaseName extracts just the database name from a DSN so the operator can
// tell which database is wedged, without putting credentials in the log.
// Anything that is not a URL-form DSN with a path is reported as unknown rather
// than echoed: a keyword/value DSN parses with the whole string as the path,
// which would put the password on stderr.
func databaseName(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil || u.Scheme == "" || !strings.HasPrefix(u.Path, "/") {
		return "unknown"
	}
	if name := strings.TrimPrefix(u.Path, "/"); name != "" {
		return name
	}
	return "unknown"
}

func ensureSSLMode(databaseURL string) string {
	if !strings.Contains(databaseURL, "sslmode=") {
		if strings.Contains(databaseURL, "?") {
			databaseURL += "&sslmode=disable"
		} else {
			databaseURL += "?sslmode=disable"
		}
	}
	return databaseURL
}
