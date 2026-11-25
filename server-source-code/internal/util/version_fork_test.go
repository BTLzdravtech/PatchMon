package util

import "testing"

// Fork: -btl builds rank above the release they are based on.
func TestCompareVersionsForkBuilds(t *testing.T) {
	ascending := []string{
		"2.1.8-rc.3", "2.1.8", "2.1.8-btl.1", "2.1.8-btl.1.4", "2.1.8-btl.2",
		"2.1.9-rc.1", "2.1.9", "2.1.9-btl.1", "2.1.10",
	}
	for i := range ascending {
		for j := range ascending {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := CompareVersions(ascending[i], ascending[j]); got != want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", ascending[i], ascending[j], got, want)
			}
		}
	}
	// "btlx" is not the fork marker; it stays an ordinary pre-release.
	if CompareVersions("2.1.8-btlx.1", "2.1.8") != -1 {
		t.Error("2.1.8-btlx.1 must stay below 2.1.8")
	}
	if CompareVersions("v2.1.8-btl.1", "2.1.8") != 1 {
		t.Error("a v prefix must not change fork ordering")
	}
}
