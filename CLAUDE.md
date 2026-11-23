# PatchMon (BTLzdravtech fork)

This is a fork of `PatchMon/PatchMon`. Remotes: `origin` is the fork, `upstream` is PatchMon, `fpenezic` is the source of some fork features.

## Upstream wins over fork customisations

When upstream ships something that replaces or overlaps a fork customisation, take the upstream solution and delete ours. This applies when merging upstream and when writing new code.

- Remove the fork's version completely: the code, its tests, its docs and any request fields or helpers that existed only for it. Do not keep a fork variant layered on top of upstream's.
- Do not rewrite upstream tests to assert fork behaviour. If an upstream test fails after a merge, the fork code is what changes.
- Keep a fork customisation only when upstream has no equivalent (for example bulk host actions and policy-driven automated patching).
- If it is unclear whether upstream's change really covers the fork's use case, ask before choosing.

## Fork-only migrations

Never add fork migrations to `server-source-code/internal/migrate/migrations/`. They go in `migrations_fork/`, which has its own version table (`schema_migrations_fork`) and runs after the upstream track. Fork migrations already run in a deployment must not be edited, renumbered or removed, even when the feature they supported is dropped.

## Merging upstream

- Do not hand-merge `server-source-code/internal/db/`; it is generated. Resolve `internal/sqlc/` first, then run `make sqlc-generate` (sqlc v1.30.0).
- After `package-lock.json` changes, run `npm ci` before Biome or vitest.
- Checks: `make check` in `server-source-code/` and `agent-source-code/`, `npx @biomejs/biome check .`, and `npm run test:run` in `frontend/`.
