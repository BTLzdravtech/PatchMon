// Fork: BTLzdravtech builds are versioned as the upstream release they are
// based on plus a "btl" identifier: 2.1.8-btl.1, and 2.1.8-btl.1.4 for four
// commits after that tag (see docker/build-fork.sh). They rank ABOVE the
// release they are based on, unlike a semver pre-release, yet below the next
// release: 2.1.8 < 2.1.8-btl.1 < 2.1.8-btl.2 < 2.1.9 < 2.1.9-btl.1.
//
// Mirrors server-source-code/internal/util/version_fork.go; keep them in step.

const isForkBuild = (pre) => pre === "btl" || pre.startsWith("btl.");

/**
 * Order two pre-release parts of versions with equal cores when either is a
 * fork build.
 * @returns {number|null} 1, -1 or 0, or null when neither is a fork build.
 */
export const compareForkBuilds = (pre1, pre2, comparePreRelease) => {
	const f1 = isForkBuild(pre1);
	const f2 = isForkBuild(pre2);
	if (f1 && f2) return comparePreRelease(pre1, pre2);
	if (f1) return 1;
	if (f2) return -1;
	return null;
};
