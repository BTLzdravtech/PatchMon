-- fork 000004: patch_policies.apt_autoremove.
--
-- Policy-level toggle consumed by automated patching (fork 000003): after a
-- successful scheduled patch_all on an apt host the agent removes the packages
-- that run left unused plus old kernels apt itself marks as removable, so
-- superseded kernels do not fill /boot and break the next kernel upgrade. The
-- agent refuses the removal when it would touch the running kernel, essential
-- or core networking packages, or more than a few packages. Manual runs do not
-- read this flag.

ALTER TABLE patch_policies ADD COLUMN IF NOT EXISTS apt_autoremove BOOLEAN NOT NULL DEFAULT false;
