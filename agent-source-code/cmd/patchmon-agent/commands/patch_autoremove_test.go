package commands

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseAptSimulation(t *testing.T) {
	out := `NOTE: This is only a simulation!
Reading package lists...
The following packages will be REMOVED:
  linux-image-6.8.0-40-generic libfoo1
Remv linux-image-6.8.0-40-generic [6.8.0-40.40]
Remv libfoo1:i386 [1.2-3]
Purg oldpkg [1.0]
Inst newpkg (2.0 Ubuntu:24.04/noble [amd64])
`
	sim := parseAptSimulation(out)
	if want := []string{"linux-image-6.8.0-40-generic", "libfoo1:i386", "oldpkg"}; !slices.Equal(sim.removed, want) {
		t.Fatalf("removed = %q, want %q", sim.removed, want)
	}
	if want := []string{"newpkg"}; !slices.Equal(sim.installed, want) {
		t.Fatalf("installed = %q, want %q", sim.installed, want)
	}
}

func TestIsKernelABIPackage(t *testing.T) {
	yes := []string{
		"linux-image-6.8.0-40-generic", "linux-modules-6.8.0-40-generic",
		"linux-modules-extra-6.8.0-40-generic", "linux-headers-6.8.0-40",
		"linux-headers-6.8.0-40-generic", "linux-image-unsigned-6.8.0-40-generic",
		"linux-image-6.1.0-18-amd64", "linux-headers-6.1.0-18-common",
		"linux-image-6.6.31+rpt-rpi-v8", "linux-hwe-6.8-tools-6.8.0-40",
	}
	no := []string{
		"linux-image-generic", "linux-generic-hwe-22.04", "linux-image-amd64",
		"linux-libc-dev", "linux-firmware", "linux-tools-common", "libfoo1", "netplan.io",
	}
	for _, n := range yes {
		if !isKernelABIPackage(n) {
			t.Errorf("isKernelABIPackage(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if isKernelABIPackage(n) {
			t.Errorf("isKernelABIPackage(%q) = true, want false", n)
		}
	}
}

func TestRunningKernelABI(t *testing.T) {
	for release, want := range map[string]string{
		"6.8.0-45-generic":  "6.8.0-45",
		"6.1.0-18-amd64":    "6.1.0-18",
		"6.6.31+rpt-rpi-v8": "6.6.31+rpt",
		"6.12.1-custom":     "6.12.1-custom",
	} {
		if got := runningKernelABI(release); got != want {
			t.Errorf("runningKernelABI(%q) = %q, want %q", release, got, want)
		}
	}
	if !belongsToKernel("linux-image-6.8.0-45-generic", "6.8.0-45") || !belongsToKernel("linux-headers-6.8.0-45", "6.8.0-45") {
		t.Fatal("running kernel packages not matched")
	}
	if belongsToKernel("linux-image-6.8.0-450-generic", "6.8.0-45") {
		t.Fatal("6.8.0-450 must not match 6.8.0-45")
	}
}

func TestSelectAptAutoremoveCandidates(t *testing.T) {
	before := map[string]bool{
		"netplan.io":                   true, // orphaned earlier, e.g. by removing ubuntu-server
		"linux-image-6.8.0-38-generic": true, // old kernel left from earlier upgrades
	}
	after := []string{"netplan.io", "linux-image-6.8.0-38-generic", "linux-image-6.8.0-40-generic", "libold1", "libold1"}
	got, leftAlone := selectAptAutoremoveCandidates(before, after)
	want := []string{"libold1", "linux-image-6.8.0-38-generic", "linux-image-6.8.0-40-generic"}
	if !slices.Equal(got, want) || leftAlone != 1 {
		t.Fatalf("got %q leftAlone %d, want %q and 1", got, leftAlone, want)
	}
}

func optional(names ...string) map[string]dpkgMeta {
	m := map[string]dpkgMeta{}
	for _, n := range names {
		m[stripAptArch(n)] = dpkgMeta{priority: "optional"}
	}
	return m
}

func TestRefuseAptAutoremove(t *testing.T) {
	const running = "6.8.0-45-generic"
	images := []string{"linux-image-6.8.0-40-generic", "linux-image-6.8.0-41-generic", "linux-image-6.8.0-45-generic"}
	manyOthers := func(n int) []string {
		var out []string
		for i := range n {
			out = append(out, "libunused"+strings.Repeat("x", i+1))
		}
		return out
	}

	tests := []struct {
		name       string
		candidates []string
		images     []string
		meta       map[string]dpkgMeta
		wantRefuse string // substring; "" means allowed
	}{
		{"old kernel and new orphan", []string{"libold1", "linux-image-6.8.0-40-generic", "linux-modules-6.8.0-40-generic"}, images, nil, ""},
		{"running kernel image", []string{"linux-image-6.8.0-45-generic"}, images, nil, "running kernel"},
		{"running kernel headers", []string{"linux-headers-6.8.0-45"}, images, nil, "running kernel"},
		{"no fallback kernel left", []string{"linux-image-6.8.0-40-generic", "linux-image-6.8.0-41-generic"}, images, nil, "fall back"},
		{"running kernel not packaged", []string{"linux-image-6.8.0-40-generic"}, []string{"linux-image-6.8.0-40-generic", "linux-image-6.8.0-41-generic"}, nil, "matches the running kernel"},
		{"networking package", []string{"netplan.io"}, images, nil, "core package"},
		{"systemd family", []string{"systemd-resolved"}, images, nil, "core package"},
		{"kernel metapackage", []string{"linux-image-generic"}, images, nil, "core package"},
		{"ubuntu metapackage", []string{"ubuntu-server"}, images, nil, "core package"},
		{"essential", []string{"libsomething"}, images, map[string]dpkgMeta{"libsomething": {essential: true}}, "Essential"},
		{"required priority", []string{"libsomething"}, images, map[string]dpkgMeta{"libsomething": {priority: "required"}}, "required"},
		{"unknown to dpkg", []string{"libsomething"}, images, map[string]dpkgMeta{}, "no record"},
		{"at the cap", manyOthers(aptAutoremoveMaxOther), images, nil, ""},
		{"over the cap", manyOthers(aptAutoremoveMaxOther + 1), images, nil, "more than the limit"},
		{"multiarch orphan", []string{"libold1:i386"}, images, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := tt.meta
			if meta == nil {
				meta = optional(tt.candidates...)
			}
			got := refuseAptAutoremove(tt.candidates, running, tt.images, meta)
			if tt.wantRefuse == "" && got != "" {
				t.Fatalf("refused: %s", got)
			}
			if tt.wantRefuse != "" && !strings.Contains(got, tt.wantRefuse) {
				t.Fatalf("reason = %q, want it to mention %q", got, tt.wantRefuse)
			}
		})
	}
}

func TestVerifyAptRemoveSimulation(t *testing.T) {
	candidates := []string{"libold1", "linux-image-6.8.0-40-generic"}
	if r := verifyAptRemoveSimulation(aptSimulation{removed: []string{"linux-image-6.8.0-40-generic", "libold1"}}, candidates); r != "" {
		t.Fatalf("exact plan refused: %s", r)
	}
	if r := verifyAptRemoveSimulation(aptSimulation{removed: []string{"libold1", "linux-image-6.8.0-40-generic", "openssh-server"}}, candidates); !strings.Contains(r, "openssh-server") {
		t.Fatalf("extra removal not refused: %q", r)
	}
	if r := verifyAptRemoveSimulation(aptSimulation{removed: candidates, installed: []string{"foo"}}, candidates); !strings.Contains(r, "install") {
		t.Fatalf("install during removal not refused: %q", r)
	}
	if r := verifyAptRemoveSimulation(aptSimulation{removed: []string{"libold1"}}, candidates); r == "" {
		t.Fatal("plan missing a candidate was accepted")
	}
}

func TestAptAutoremoveRemoveArgs(t *testing.T) {
	got := aptAutoremoveRemoveArgs(true, []string{"libold1", "linux-image-6.8.0-40-generic"})
	want := []string{
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold",
		"-o", "Acquire::Retries=0", "-o", "APT::Get::AutomaticRemove=false",
		"remove", "-y", "libold1", "linux-image-6.8.0-40-generic",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"--purge", "purge", "autoremove", "--allow-remove-essential"} {
		if slices.Contains(got, forbidden) {
			t.Fatalf("args = %q must not contain %q", got, forbidden)
		}
	}
	before := slices.Clone(dpkgConfOptions)
	_ = aptAutoremoveRemoveArgs(false, []string{"x"})
	if !slices.Equal(dpkgConfOptions, before) {
		t.Fatalf("dpkgConfOptions changed to %q", dpkgConfOptions)
	}
}

// fakeApt answers the read-only queries finish makes.
type fakeApt struct {
	beforeSim, afterSim, removeSim string
	images                         string
	meta                           map[string]dpkgMeta
	failAfter                      bool
}

func (f *fakeApt) install(t *testing.T, release string) {
	t.Helper()
	origQuery, origRelease := aptQuery, runningKernelRelease
	calls := 0
	aptQuery = func(_ context.Context, _ []string, name string, args ...string) (string, error) {
		switch {
		case name == "apt-get" && slices.Equal(args, []string{"-s", "autoremove"}):
			calls++
			if calls == 1 {
				return f.beforeSim, nil
			}
			if f.failAfter {
				return "", errors.New("dpkg was interrupted")
			}
			return f.afterSim, nil
		case name == "apt-get" && slices.Contains(args, "remove"):
			return f.removeSim, nil
		case name == "dpkg-query" && slices.Contains(args, "linux-image-*"):
			return f.images, nil
		case name == "dpkg-query":
			var b strings.Builder
			for _, n := range args[2:] {
				m, ok := f.meta[n]
				if !ok {
					m = dpkgMeta{priority: "optional"}
				}
				ess := "no"
				if m.essential {
					ess = "yes"
				}
				b.WriteString(n + "\t" + ess + "\t" + m.priority + "\n")
			}
			return b.String(), nil
		}
		t.Fatalf("unexpected query %s %q", name, args)
		return "", nil
	}
	runningKernelRelease = func() (string, error) { return release, nil }
	t.Cleanup(func() { aptQuery, runningKernelRelease = origQuery, origRelease })
}

type recordedStep struct {
	name string
	args []string
}

func recordingRunner(steps *[]recordedStep, fail error) patchStepRunner {
	return func(_ bool, _, _ string, name string, args ...string) (error, bool) {
		*steps = append(*steps, recordedStep{name, args})
		if fail != nil {
			return fail, true
		}
		return nil, false
	}
}

const threeKernels = "linux-image-6.8.0-40-generic\tii \nlinux-image-6.8.0-41-generic\tii \nlinux-image-6.8.0-45-generic\tii \nlinux-image-generic\tii \n"

func runFinish(t *testing.T, f *fakeApt, fail error) (string, []recordedStep, error) {
	t.Helper()
	f.install(t, "6.8.0-45-generic")
	var full strings.Builder
	sink := newStreamSink(nil, "run", &full)
	plan := beginAptAutoremove(context.Background(), sink, nil, false)
	if plan == nil {
		t.Fatalf("no plan; output %q", full.String())
	}
	var steps []recordedStep
	err := plan.finish(context.Background(), sink, nil, true, recordingRunner(&steps, fail))
	return full.String(), steps, err
}

func TestAptAutoremoveFinishRemovesOnlyApproved(t *testing.T) {
	f := &fakeApt{
		beforeSim: "Remv netplan.io [1.0]\nRemv linux-image-6.8.0-38-generic [6.8.0-38.38]\n",
		afterSim:  "Remv netplan.io [1.0]\nRemv linux-image-6.8.0-38-generic [6.8.0-38.38]\nRemv linux-image-6.8.0-40-generic [6.8.0-40.40]\nRemv libold1 [1.0]\n",
		removeSim: "Remv libold1 [1.0]\nRemv linux-image-6.8.0-38-generic [6.8.0-38.38]\nRemv linux-image-6.8.0-40-generic [6.8.0-40.40]\n",
		images:    "linux-image-6.8.0-38-generic\tii \n" + threeKernels,
	}
	out, steps, err := runFinish(t, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %+v; output %q", steps, out)
	}
	names := steps[0].args[len(steps[0].args)-3:]
	if want := []string{"libold1", "linux-image-6.8.0-38-generic", "linux-image-6.8.0-40-generic"}; !slices.Equal(names, want) {
		t.Fatalf("removing %q, want %q", names, want)
	}
	if slices.Contains(steps[0].args, "netplan.io") {
		t.Fatal("a package that was already unused before the run was removed")
	}
	if !strings.Contains(out, "1 package(s) that were already unused left alone") {
		t.Fatalf("output %q does not report the package left alone", out)
	}
}

func TestAptAutoremoveFinishSkips(t *testing.T) {
	tests := []struct {
		name string
		f    fakeApt
		want string
	}{
		{
			name: "apt would take more with it",
			f: fakeApt{
				afterSim:  "Remv libold1 [1.0]\n",
				removeSim: "Remv libold1 [1.0]\nRemv openssh-server [1:9.6]\n",
				images:    threeKernels,
			},
			want: "apt would also remove openssh-server",
		},
		{
			name: "running kernel offered for removal",
			f: fakeApt{
				afterSim: "Remv linux-image-6.8.0-45-generic [6.8.0-45.45]\n",
				images:   threeKernels,
			},
			want: "running kernel",
		},
		{
			name: "core package newly unused",
			f: fakeApt{
				afterSim: "Remv netplan.io [1.0]\n",
				images:   threeKernels,
			},
			want: "core package",
		},
		{
			name: "after-upgrade listing fails",
			f:    fakeApt{failAfter: true, images: threeKernels},
			want: "could not list removable packages after the upgrade",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, steps, err := runFinish(t, &tt.f, nil)
			if err != nil {
				t.Fatalf("a skip must not fail the run: %v", err)
			}
			if len(steps) != 0 {
				t.Fatalf("removed despite the skip: %+v", steps)
			}
			if !strings.Contains(out, "apt autoremove skipped") || !strings.Contains(out, tt.want) {
				t.Fatalf("output %q, want a skip mentioning %q", out, tt.want)
			}
		})
	}
}

func TestAptAutoremoveFinishNothingToRemove(t *testing.T) {
	out, steps, err := runFinish(t, &fakeApt{beforeSim: "Remv netplan.io [1.0]\n", afterSim: "Remv netplan.io [1.0]\n", images: threeKernels}, nil)
	if err != nil || len(steps) != 0 || !strings.Contains(out, "nothing to remove") {
		t.Fatalf("err %v, steps %+v, output %q", err, steps, out)
	}
}

func TestAptAutoremoveFinishRemovalFailureFailsRun(t *testing.T) {
	boom := errors.New("apt-get remove (autoremove) failed: exit status 100")
	f := &fakeApt{afterSim: "Remv libold1 [1.0]\n", removeSim: "Remv libold1 [1.0]\n", images: threeKernels}
	if _, _, err := runFinish(t, f, boom); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the removal error", err)
	}
}

func TestBeginAptAutoremoveDryRunSkips(t *testing.T) {
	var full strings.Builder
	if plan := beginAptAutoremove(context.Background(), newStreamSink(nil, "run", &full), nil, true); plan != nil {
		t.Fatal("dry run produced a plan")
	}
	if !strings.Contains(full.String(), "dry run") {
		t.Fatalf("output %q does not explain the skip", full.String())
	}
}
