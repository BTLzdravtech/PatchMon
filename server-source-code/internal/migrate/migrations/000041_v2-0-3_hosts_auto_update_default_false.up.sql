-- 000041: change hosts.auto_update default to false.
--
-- Rationale: agent self-update fires on every server-side trigger when the
-- per-host flag is true. Newly enrolled hosts inherited true, which meant
-- any "Force update all" or scheduled rollout would touch hosts the operator
-- never opted in. Flipping the default to false makes auto-update opt-in
-- for new hosts.
--
-- Existing hosts are left untouched: operators who have explicitly enabled
-- auto-update on hosts they manage keep that setting. Only the default for
-- INSERTs without an auto_update column changes.

ALTER TABLE hosts ALTER COLUMN auto_update SET DEFAULT false;
