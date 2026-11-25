package queue

import (
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

func TestAutoPatchRunOpts(t *testing.T) {
	v := func(s string) *string { return &s }
	tests := []struct {
		name        string
		osType      string
		version     *string
		wantSkip    bool
		wantWindows bool
	}{
		{"linux needs no options", "Debian", v("2.1.8"), false, false},
		{"linux ignores agent version", "Ubuntu", nil, false, false},
		{"windows on a current agent", "Windows Server 2022", v("2.1.8"), false, true},
		{"windows on the minimum agent", "windows", v(MinWindowsAutoPatchAgentVersion), false, true},
		{"windows on a pre-release of the minimum", "Windows 11", v(MinWindowsAutoPatchAgentVersion + "-rc.1"), false, true},
		{"windows on an older agent", "Windows Server 2019", v("2.1.3"), true, false},
		{"windows with no reported agent version", "Windows Server 2019", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, skip := autoPatchRunOpts(tt.osType, tt.version)
			if (skip != "") != tt.wantSkip {
				t.Fatalf("skip = %q, wantSkip %v", skip, tt.wantSkip)
			}
			if !tt.wantWindows {
				if opts != nil {
					t.Fatalf("opts = %+v, want nil", opts)
				}
				return
			}
			if opts == nil || opts.WindowsIncludeUpdates == nil || opts.WindowsIncludeApps == nil {
				t.Fatalf("opts = %+v, want an explicit windows scope", opts)
			}
			// Automated runs install Windows Updates only, never winget apps.
			if !*opts.WindowsIncludeUpdates || *opts.WindowsIncludeApps {
				t.Fatalf("scope updates=%v apps=%v, want updates only", *opts.WindowsIncludeUpdates, *opts.WindowsIncludeApps)
			}
		})
	}
}

// The policy's auto-reboot flag has to reach Windows agents too, alongside
// the updates-only scope the dispatcher stored on the run.
func TestBuildRunPatchMessageWindowsCarriesRebootFlag(t *testing.T) {
	updates, apps := true, false
	run := &db.GetPatchRunByIDRow{WindowsIncludeUpdates: &updates, WindowsIncludeApps: &apps}

	msg := buildRunPatchMessage(RunPatchPayload{PatchType: "patch_all", RebootIfRequired: true}, run, true, nil)
	if msg["reboot_if_required"] != true {
		t.Fatalf("reboot_if_required = %v, want true; msg = %+v", msg["reboot_if_required"], msg)
	}
	if msg["include_updates"] != true || msg["include_apps"] != false {
		t.Fatalf("scope = updates %v apps %v, want updates only", msg["include_updates"], msg["include_apps"])
	}

	msg = buildRunPatchMessage(RunPatchPayload{PatchType: "patch_all"}, run, true, nil)
	if _, ok := msg["reboot_if_required"]; ok {
		t.Fatalf("reboot_if_required present without the policy flag: %+v", msg)
	}
}

// The policy apt autoremove flag reaches Linux/BSD agents on patch_all only:
// selected-package runs and Windows hosts never carry it.
func TestBuildRunPatchMessageAptAutoremove(t *testing.T) {
	tests := []struct {
		name    string
		payload RunPatchPayload
		windows bool
		want    bool
	}{
		{"patch_all with the policy flag", RunPatchPayload{PatchType: "patch_all", AptAutoremove: true}, false, true},
		{"patch_all without the policy flag", RunPatchPayload{PatchType: "patch_all"}, false, false},
		{"selected packages ignore the flag", RunPatchPayload{PatchType: "patch_package", PackageNames: []string{"curl"}, AptAutoremove: true}, false, false},
		{"windows ignores the flag", RunPatchPayload{PatchType: "patch_all", AptAutoremove: true}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := buildRunPatchMessage(tt.payload, &db.GetPatchRunByIDRow{}, tt.windows, nil)
			got, ok := msg["apt_autoremove"]
			if tt.want && got != true {
				t.Fatalf("aptAutoremove = %v, want true; msg = %+v", got, msg)
			}
			if !tt.want && ok {
				t.Fatalf("aptAutoremove present: %+v", msg)
			}
		})
	}
}
