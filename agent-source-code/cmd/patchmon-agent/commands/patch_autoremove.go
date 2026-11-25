package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// Fork: guarded apt autoremove after an automated Patch all.
//
// `apt-get upgrade --with-new-pkgs` installs every new kernel ABI as a new
// package and nothing removes the old ones, so a separate /boot fills up and a
// later kernel upgrade fails half-way. A plain `apt-get autoremove` fixes that
// but trusts apt's "automatically installed" marks, which are often wrong:
// removing a metapackage such as ubuntu-server leaves netplan.io, iproute2 and
// friends marked removable, and hosts have come back from the next reboot
// without a network. So, like unattended-upgrades' default
// (Remove-New-Unused-Dependencies, not Remove-Unused-Dependencies), the agent
// removes only:
//
//   - packages that became removable during this run (snapshot before the
//     upgrade, compare after), and
//   - old kernel packages that apt itself marks removable. apt decides which
//     kernels to keep (running + newest ones); the agent then double-checks.
//
// Packages that were already removable before the run are never touched. The
// removal is skipped, with the reason in the run output, if it would remove the
// running kernel, leave no fallback kernel, remove an Essential or
// Priority: required package or anything on the core deny list, remove more
// than aptAutoremoveMaxOther non-kernel packages, or if apt's own simulation of
// the exact removal would take anything else with it. A skip never fails the
// run; a removal that apt starts and fails does, so the policy reboot does not
// reboot a host in the middle of a broken dpkg state.
//
// Configuration files are kept (remove, not purge).

// aptAutoremoveMaxOther caps the non-kernel packages one run may remove. An
// upgrade that orphans more than this is unusual enough for a human to look.
const aptAutoremoveMaxOther = 25

// aptNoAutoRemove stops `apt-get remove` from also autoremoving when the host
// sets APT::Get::AutomaticRemove, which would bypass every check here.
var aptNoAutoRemove = []string{"-o", "APT::Get::AutomaticRemove=false"}

// aptQuery runs a read-only apt/dpkg command and returns its stdout.
var aptQuery = func(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
	}
	return string(out), err
}

// runningKernelRelease returns `uname -r`.
var runningKernelRelease = func() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "", err
	}
	r := strings.TrimSpace(string(b))
	if r == "" {
		return "", errors.New("empty kernel release")
	}
	return r, nil
}

// aptAutoremovePlan carries the pre-upgrade snapshot into finish.
type aptAutoremovePlan struct {
	before map[string]bool
}

func aptAutoremoveSkip(sink *streamSink, reason string) {
	logger.WithField("reason", reason).Warn("apt autoremove skipped")
	sink.WriteString(fmt.Sprintf("\n[patchmon] apt autoremove skipped: %s. Nothing was removed.\n", reason))
	sink.Flush()
}

// beginAptAutoremove records what apt would already autoremove before the
// upgrade. It returns nil, after explaining why, when there will be no cleanup.
func beginAptAutoremove(ctx context.Context, sink *streamSink, env []string, dryRun bool) *aptAutoremovePlan {
	if dryRun {
		aptAutoremoveSkip(sink, "a dry run cannot tell which packages the upgrade will leave unused")
		return nil
	}
	out, err := aptQuery(ctx, env, "apt-get", "-s", "autoremove")
	if err != nil {
		aptAutoremoveSkip(sink, "could not list removable packages before the upgrade ("+err.Error()+")")
		return nil
	}
	before := map[string]bool{}
	for _, name := range parseAptSimulation(out).removed {
		before[name] = true
	}
	return &aptAutoremovePlan{before: before}
}

// finish removes what the upgrade left unused plus old kernels, after the
// checks. It returns an error only when the removal itself fails.
func (p *aptAutoremovePlan) finish(ctx context.Context, sink *streamSink, env []string, disableRetries bool, runStep patchStepRunner) error {
	out, err := aptQuery(ctx, env, "apt-get", "-s", "autoremove")
	if err != nil {
		aptAutoremoveSkip(sink, "could not list removable packages after the upgrade ("+err.Error()+")")
		return nil
	}
	candidates, leftAlone := selectAptAutoremoveCandidates(p.before, parseAptSimulation(out).removed)
	if len(candidates) == 0 {
		sink.WriteString(fmt.Sprintf("\n[patchmon] apt autoremove: nothing to remove (%d package(s) that were already unused left alone).\n", leftAlone))
		sink.Flush()
		return nil
	}

	release, err := runningKernelRelease()
	if err != nil {
		aptAutoremoveSkip(sink, "cannot read the running kernel release ("+err.Error()+")")
		return nil
	}
	images, err := installedKernelImages(ctx, env)
	if err != nil {
		aptAutoremoveSkip(sink, "cannot list installed kernels ("+err.Error()+")")
		return nil
	}
	meta, err := dpkgPackageMeta(ctx, env, candidates)
	if err != nil {
		aptAutoremoveSkip(sink, "cannot read package priorities ("+err.Error()+")")
		return nil
	}
	if reason := refuseAptAutoremove(candidates, release, images, meta); reason != "" {
		aptAutoremoveSkip(sink, reason)
		return nil
	}

	// Let apt resolve the exact removal and confirm it touches nothing else.
	simArgs := append(append([]string{"-s"}, aptNoAutoRemove...), "remove")
	simOut, err := aptQuery(ctx, env, "apt-get", append(simArgs, candidates...)...)
	if err != nil {
		aptAutoremoveSkip(sink, "apt could not simulate the removal ("+err.Error()+")")
		return nil
	}
	if reason := verifyAptRemoveSimulation(parseAptSimulation(simOut), candidates); reason != "" {
		aptAutoremoveSkip(sink, reason)
		return nil
	}

	kernels := 0
	for _, c := range candidates {
		if isKernelABIPackage(c) {
			kernels++
		}
	}
	sink.WriteString(fmt.Sprintf("\n[patchmon] apt autoremove: removing %d package(s) (%d old kernel, %d left unused by this run); %d package(s) that were already unused left alone.\n",
		len(candidates), kernels, len(candidates)-kernels, leftAlone))
	sink.Flush()
	if err, abort := runStep(false, "apt-get remove", "apt-get remove (autoremove) failed: %w", "apt-get", aptAutoremoveRemoveArgs(disableRetries, candidates)...); abort {
		return err
	}
	return nil
}

// aptAutoremoveRemoveArgs builds the apt-get arguments for removing exactly
// the approved packages.
func aptAutoremoveRemoveArgs(disableRetries bool, names []string) []string {
	args := slices.Clone(dpkgConfOptions)
	if disableRetries {
		args = append(args, aptRetriesOff...)
	}
	args = append(args, aptNoAutoRemove...)
	args = append(args, "remove", "-y")
	return append(args, names...)
}

type aptSimulation struct {
	removed   []string // Remv and Purg lines
	installed []string // Inst lines
}

// parseAptSimulation reads `apt-get -s` output ("Remv name [version]").
func parseAptSimulation(out string) aptSimulation {
	var sim aptSimulation
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "Remv", "Purg":
			sim.removed = append(sim.removed, fields[1])
		case "Inst":
			sim.installed = append(sim.installed, fields[1])
		}
	}
	return sim
}

// selectAptAutoremoveCandidates keeps what became removable during this run
// and every removable kernel ABI package, sorted. leftAlone counts removable
// packages kept because they were already removable before the run.
func selectAptAutoremoveCandidates(before map[string]bool, after []string) (candidates []string, leftAlone int) {
	seen := map[string]bool{}
	for _, name := range after {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !before[name] || isKernelABIPackage(name) {
			candidates = append(candidates, name)
		} else {
			leftAlone++
		}
	}
	slices.Sort(candidates)
	return candidates, leftAlone
}

func stripAptArch(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i]
	}
	return name
}

// kernelVersionInName finds an upstream kernel version (6.8.0, 6.1.0) as its
// own dash-separated part of a package name.
var kernelVersionInName = regexp.MustCompile(`-\d+\.\d+\.\d+([-+.]|$)`)

// isKernelABIPackage reports a package tied to one kernel build, such as
// linux-image-6.8.0-45-generic, linux-modules-extra-6.8.0-45-generic,
// linux-headers-6.8.0-45 or linux-image-6.1.0-18-amd64. Metapackages
// (linux-image-generic, linux-generic-hwe-22.04) are not.
func isKernelABIPackage(name string) bool {
	name = stripAptArch(name)
	return strings.HasPrefix(name, "linux-") && kernelVersionInName.MatchString(name)
}

var kernelImagePackage = regexp.MustCompile(`^linux-image-(unsigned-)?\d`)

// runningKernelABI reduces `uname -r` to the part every package of that build
// carries: 6.8.0-45-generic → 6.8.0-45, 6.1.0-18-amd64 → 6.1.0-18,
// 6.6.31+rpt-rpi-v8 → 6.6.31+rpt. Anything else is used whole.
var runningKernelABIPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+-\d+|\d+\.\d+\.\d+\+[a-z0-9]+)`)

func runningKernelABI(release string) string {
	if m := runningKernelABIPattern.FindString(release); m != "" {
		return m
	}
	return release
}

// belongsToKernel reports whether a package name carries the given ABI as a
// whole part (6.8.0-45 matches -6.8.0-45-generic, not -6.8.0-450).
func belongsToKernel(name, abi string) bool {
	name = stripAptArch(name)
	for i := 0; ; {
		j := strings.Index(name[i:], "-"+abi)
		if j < 0 {
			return false
		}
		end := i + j + 1 + len(abi)
		if end == len(name) || name[end] == '-' || name[end] == '.' {
			return true
		}
		i += j + 1
	}
}

// aptAutoremoveDenyExact and aptAutoremoveDenyPrefixes are packages an
// unattended run must never remove, whatever apt's marks say: package
// management, boot, storage, networking and remote access. Kernel ABI packages
// are checked separately and are not subject to the linux- prefixes.
var aptAutoremoveDenyExact = map[string]bool{
	"apt": true, "dpkg": true, "sudo": true, "login": true, "passwd": true, "bash": true,
	"coreutils": true, "util-linux": true, "libc6": true, "ca-certificates": true,
	"netplan.io": true, "ifupdown": true, "ifupdown2": true, "network-manager": true,
	"iproute2": true, "netbase": true, "resolvconf": true, "isc-dhcp-client": true,
	"dhcpcd-base": true, "dhcpcd5": true, "openssh-server": true, "openssh-sftp-server": true,
	"cloud-init": true, "chrony": true, "initramfs-tools": true, "dracut": true,
	"lvm2": true, "mdadm": true, "cryptsetup": true, "e2fsprogs": true,
	"xfsprogs": true, "btrfs-progs": true,
}

var aptAutoremoveDenyPrefixes = []string{
	"systemd", "ubuntu-", "grub", "shim", "linux-image-", "linux-headers-",
	"linux-modules-", "linux-generic", "linux-virtual", "linux-signed",
	"linux-firmware", "linux-base", "firmware-", "patchmon", "univention-",
}

func aptAutoremoveDenied(name string) bool {
	name = stripAptArch(name)
	if aptAutoremoveDenyExact[name] {
		return true
	}
	for _, p := range aptAutoremoveDenyPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

type dpkgMeta struct {
	essential bool
	priority  string
}

// refuseAptAutoremove returns why the candidates must not be removed, or "".
func refuseAptAutoremove(candidates []string, release string, installedImages []string, meta map[string]dpkgMeta) string {
	abi := runningKernelABI(release)
	var kernels, others []string
	for _, c := range candidates {
		if isKernelABIPackage(c) {
			kernels = append(kernels, c)
		} else {
			others = append(others, c)
		}
	}

	for _, c := range candidates {
		m, ok := meta[stripAptArch(c)]
		if !ok {
			return fmt.Sprintf("dpkg has no record of %s", c)
		}
		if m.essential {
			return fmt.Sprintf("%s is an Essential package", c)
		}
		if m.priority == "required" {
			return fmt.Sprintf("%s has Priority: required", c)
		}
	}
	for _, c := range others {
		if aptAutoremoveDenied(c) {
			return fmt.Sprintf("%s is a core package PatchMon never removes unattended", c)
		}
	}
	if len(others) > aptAutoremoveMaxOther {
		return fmt.Sprintf("the upgrade left %d non-kernel packages unused, more than the limit of %d; review them with apt-get -s autoremove", len(others), aptAutoremoveMaxOther)
	}

	if len(kernels) == 0 {
		return ""
	}
	for _, k := range kernels {
		if belongsToKernel(k, abi) {
			return fmt.Sprintf("%s belongs to the running kernel %s", k, release)
		}
	}
	removing := map[string]bool{}
	for _, c := range candidates {
		removing[stripAptArch(c)] = true
	}
	runningPresent, otherInstalled, otherRemaining := false, 0, 0
	for _, img := range installedImages {
		running := belongsToKernel(img, abi)
		if running {
			runningPresent = true
			continue
		}
		otherInstalled++
		if !removing[stripAptArch(img)] {
			otherRemaining++
		}
	}
	if !runningPresent {
		return fmt.Sprintf("no installed kernel package matches the running kernel %s", release)
	}
	if otherInstalled > 0 && otherRemaining == 0 {
		return "it would leave no kernel to fall back to besides the running one"
	}
	return ""
}

// verifyAptRemoveSimulation checks apt's own plan for the removal: it must
// remove exactly the approved packages and install nothing.
func verifyAptRemoveSimulation(sim aptSimulation, candidates []string) string {
	if len(sim.installed) > 0 {
		return fmt.Sprintf("apt would install %s while removing", strings.Join(sim.installed, ", "))
	}
	approved := map[string]bool{}
	for _, c := range candidates {
		approved[stripAptArch(c)] = true
	}
	var extra []string
	removed := map[string]bool{}
	for _, r := range sim.removed {
		removed[stripAptArch(r)] = true
		if !approved[stripAptArch(r)] {
			extra = append(extra, r)
		}
	}
	if len(extra) > 0 {
		return fmt.Sprintf("apt would also remove %s", strings.Join(extra, ", "))
	}
	for _, c := range candidates {
		if !removed[stripAptArch(c)] {
			return fmt.Sprintf("apt's plan does not remove %s as expected", c)
		}
	}
	return ""
}

// installedKernelImages lists installed linux-image-<version> packages.
func installedKernelImages(ctx context.Context, env []string) ([]string, error) {
	out, err := aptQuery(ctx, env, "dpkg-query", "-W", "-f=${binary:Package}\t${db:Status-Abbrev}\n", "linux-image-*")
	if err != nil {
		var exitErr *exec.ExitError
		// dpkg-query exits 1 when nothing matches the pattern.
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || strings.TrimSpace(out) != "" {
			return nil, err
		}
	}
	var images []string
	for _, line := range strings.Split(out, "\n") {
		name, status, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(status, "ii") {
			continue
		}
		if kernelImagePackage.MatchString(stripAptArch(name)) {
			images = append(images, name)
		}
	}
	return images, nil
}

// dpkgPackageMeta reads Essential and Priority for the given packages.
func dpkgPackageMeta(ctx context.Context, env []string, names []string) (map[string]dpkgMeta, error) {
	args := append([]string{"-W", "-f=${binary:Package}\t${Essential}\t${Priority}\n"}, names...)
	out, err := aptQuery(ctx, env, "dpkg-query", args...)
	if err != nil {
		return nil, err
	}
	meta := map[string]dpkgMeta{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		meta[stripAptArch(parts[0])] = dpkgMeta{essential: parts[1] == "yes", priority: strings.TrimSpace(parts[2])}
	}
	return meta, nil
}

// patchStepRunner is runPatch's runStep: it streams one command into the run
// output and returns (terminal error, abort).
type patchStepRunner func(isDryRunStep bool, errTag, errFmt, name string, args ...string) (error, bool)
