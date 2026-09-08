-- fork 000001: patch_policies.auto_reboot.
--
-- Policy-level toggle consumed by automated patching (fork 000002): after a
-- successful scheduled patch_all the agent reboots iff the host still reports
-- a pending reboot. Manual runs and the on-demand reboot endpoints
-- (POST /hosts/:id/reboot, POST /hosts/bulk/reboot) do not read this flag.

ALTER TABLE patch_policies ADD COLUMN IF NOT EXISTS auto_reboot BOOLEAN NOT NULL DEFAULT false;
