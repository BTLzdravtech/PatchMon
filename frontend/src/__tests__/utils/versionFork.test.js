import { describe, expect, it } from "vitest";
import { compareVersions, isUpdateAvailable } from "../../utils/version";

// Fork: mirrors version_fork_test.go in the server.
describe("fork -btl versions", () => {
	const ascending = [
		"2.1.8-rc.3",
		"2.1.8",
		"2.1.8-btl.1",
		"2.1.8-btl.1.4",
		"2.1.8-btl.2",
		"2.1.9-rc.1",
		"2.1.9",
		"2.1.9-btl.1",
		"2.1.10",
	];

	it("orders fork builds above their release and below the next", () => {
		ascending.forEach((a, i) => {
			ascending.forEach((b, j) => {
				expect(compareVersions(a, b)).toBe(Math.sign(i - j));
			});
		});
	});

	it("leaves look-alike identifiers as ordinary pre-releases", () => {
		expect(compareVersions("2.1.8-btlx.1", "2.1.8")).toBe(-1);
	});

	it("offers the fork build as an update to the upstream release", () => {
		expect(isUpdateAvailable("2.1.8", "2.1.8-btl.1")).toBe(true);
		expect(isUpdateAvailable("2.1.8-btl.1", "2.1.8")).toBe(false);
		expect(isUpdateAvailable("2.1.8-btl.2", "2.1.9")).toBe(true);
	});
});
