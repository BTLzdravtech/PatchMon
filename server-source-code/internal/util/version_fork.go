package util

import "strings"

// Fork: BTLzdravtech builds are versioned as the upstream release they are
// based on plus a "btl" identifier: 2.1.8-btl.1, and 2.1.8-btl.1.4 for four
// commits after that tag (see docker/build-fork.sh). Agents only update to a
// strictly higher version, so a fork build must rank ABOVE the release it is
// based on, unlike a semver pre-release, yet below the next release:
//
//	2.1.8 < 2.1.8-btl.1 < 2.1.8-btl.1.4 < 2.1.8-btl.2 < 2.1.9 < 2.1.9-btl.1
//
// frontend/src/utils/versionFork.js implements the same rule; keep them in step.

// isForkBuild reports a pre-release part that marks a fork build.
func isForkBuild(pre string) bool {
	return pre == "btl" || strings.HasPrefix(pre, "btl.")
}

// compareForkBuilds orders two versions with equal cores when either is a
// fork build. ok is false when neither is, leaving semver ordering in charge.
func compareForkBuilds(pre1, pre2 string) (c int, ok bool) {
	f1, f2 := isForkBuild(pre1), isForkBuild(pre2)
	switch {
	case f1 && f2:
		return comparePreRelease(pre1, pre2), true
	case f1:
		return 1, true
	case f2:
		return -1, true
	}
	return 0, false
}
