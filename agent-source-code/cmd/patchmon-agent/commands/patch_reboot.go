package commands

import (
	"patchmon-agent/internal/packages"
	"patchmon-agent/internal/system"
)

// Policy-driven post-patch reboot (fork feature: automated patching with
// "reboot after patching if the host requires it"). Shared by the Linux/BSD
// and Windows patch paths so both apply exactly the same rule.

const postPatchRebootReason = "PatchMon post-patch reboot (policy)"

// Indirections so tests can drive the decision without touching the host.
var (
	postPatchRebootCheck = func() (bool, string) {
		return system.New(logger).CheckRebootRequired()
	}
	// 1-minute delay: gives logged-in users the shutdown notice and lets the
	// final WebSocket/HTTP exchanges drain before the connection drops.
	postPatchRebootExec = func() error {
		return system.New(logger).ExecuteReboot(1, postPatchRebootReason)
	}
	runPatchWindowsFn = runPatchWindows
)

// rebootAfterPatchIfRequired schedules a reboot only when the host actually
// reports one pending. Call it after a successful, non-dry-run patch run that
// carried the policy's reboot flag.
func rebootAfterPatchIfRequired() {
	needs, reason := postPatchRebootCheck()
	if !needs {
		logger.Info("Post-patch reboot requested by policy but host does not need one; skipping")
		return
	}
	logger.WithField("reason", reason).Info("Post-patch reboot required by policy; scheduling reboot")
	if err := postPatchRebootExec(); err != nil {
		logger.WithError(err).Warn("Post-patch reboot scheduling failed")
	}
}

// runPatchWindowsWithReboot runs a Windows patch run and then applies the
// policy reboot. It wraps runPatchWindows rather than changing it so the
// upstream function keeps its signature. As on Linux, a failed, stopped or
// dry run never reboots.
func runPatchWindowsWithReboot(patchRunID string, req packages.WindowsPatchRequest, parseErr string, rebootIfRequired bool) error {
	err := runPatchWindowsFn(patchRunID, req, parseErr)
	if err != nil || !rebootIfRequired || req.DryRun {
		return err
	}
	rebootAfterPatchIfRequired()
	return nil
}
