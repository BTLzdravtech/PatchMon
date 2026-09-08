# Fork migrations

Migrations in this directory belong to the BTLzdravtech fork only. They run
after the upstream set in `../migrations/` and are tracked in their own table,
`schema_migrations_fork`, so upstream can keep adding `0000NN_*` files without
ever colliding with ours.

Why a separate track: golang-migrate stores a single current version per
table, not a list of applied files. If fork migrations shared upstream's
numbering, every upstream release that reused a number would either refuse to
load (duplicate version on a fresh database) or be silently skipped (deployed
database already past that number).

Rules for files here:

- Number from `000001` upwards, independently of upstream.
- Write them idempotent (`IF NOT EXISTS`, `IF EXISTS`) so that an upstream
  migration later adding the same object can be reconciled with a one-line
  edit rather than a data fix.
- When a feature is merged upstream with its own migration, keep the fork file
  in place (the column already exists on our databases) and make upstream's
  copy idempotent in the merge if it is not already.

Keep the sqlc schema in `internal/sqlc/schema/schema.sql` in sync as usual.
