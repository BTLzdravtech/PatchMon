package commands

import (
	"errors"
	"testing"

	"patchmon-agent/internal/packages"
)

func TestRunPatchWindowsWithReboot(t *testing.T) {
	failed := errors.New("install failed")
	tests := []struct {
		name         string
		flag         bool
		dryRun       bool
		runErr       error
		rebootNeeded bool
		wantCheck    bool
		wantReboot   bool
	}{
		{"policy flag and reboot pending", true, false, nil, true, true, true},
		{"policy flag but nothing pending", true, false, nil, false, true, false},
		{"no policy flag", false, false, nil, true, false, false},
		{"dry run never reboots", true, true, nil, true, false, false},
		{"failed run never reboots", true, false, failed, true, false, false},
		{"stopped run never reboots", true, false, errPatchRunStopped, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevRun, prevCheck, prevExec := runPatchWindowsFn, postPatchRebootCheck, postPatchRebootExec
			t.Cleanup(func() {
				runPatchWindowsFn, postPatchRebootCheck, postPatchRebootExec = prevRun, prevCheck, prevExec
			})

			var gotReq packages.WindowsPatchRequest
			checked, rebooted := false, false
			runPatchWindowsFn = func(_ string, req packages.WindowsPatchRequest, _ string) error {
				gotReq = req
				return tt.runErr
			}
			postPatchRebootCheck = func() (bool, string) {
				checked = true
				return tt.rebootNeeded, "pending file rename operations"
			}
			postPatchRebootExec = func() error {
				rebooted = true
				return nil
			}

			req := packages.WindowsPatchRequest{PatchAll: true, IncludeUpdates: true, DryRun: tt.dryRun}
			err := runPatchWindowsWithReboot("11111111-1111-4111-8111-111111111111", req, "", tt.flag)
			if !errors.Is(err, tt.runErr) {
				t.Fatalf("err = %v, want %v", err, tt.runErr)
			}
			if !gotReq.PatchAll || !gotReq.IncludeUpdates {
				t.Fatalf("request not passed through: %+v", gotReq)
			}
			if checked != tt.wantCheck {
				t.Fatalf("reboot check ran = %v, want %v", checked, tt.wantCheck)
			}
			if rebooted != tt.wantReboot {
				t.Fatalf("reboot scheduled = %v, want %v", rebooted, tt.wantReboot)
			}
		})
	}
}

// A failure to schedule the reboot must not turn a successful patch run into
// a failed one: the patching did happen.
func TestRunPatchWindowsWithRebootIgnoresScheduleFailure(t *testing.T) {
	prevRun, prevCheck, prevExec := runPatchWindowsFn, postPatchRebootCheck, postPatchRebootExec
	t.Cleanup(func() {
		runPatchWindowsFn, postPatchRebootCheck, postPatchRebootExec = prevRun, prevCheck, prevExec
	})
	runPatchWindowsFn = func(string, packages.WindowsPatchRequest, string) error { return nil }
	postPatchRebootCheck = func() (bool, string) { return true, "pending" }
	postPatchRebootExec = func() error { return errors.New("shutdown refused") }

	if err := runPatchWindowsWithReboot("11111111-1111-4111-8111-111111111111", packages.WindowsPatchRequest{PatchAll: true}, "", true); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}
