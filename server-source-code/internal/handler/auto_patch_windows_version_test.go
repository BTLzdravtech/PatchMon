package handler

import (
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/queue"
)

// The automated-patching dispatcher lives in the queue package, which cannot
// import this one, so it carries its own copy of the minimum Windows agent
// version. Fail here if upstream moves the real one.
func TestAutoPatchWindowsMinAgentMatchesManualPath(t *testing.T) {
	if queue.MinWindowsAutoPatchAgentVersion != MinWindowsPatchAgentVersion {
		t.Fatalf("queue.MinWindowsAutoPatchAgentVersion = %q, handler.MinWindowsPatchAgentVersion = %q; keep them equal",
			queue.MinWindowsAutoPatchAgentVersion, MinWindowsPatchAgentVersion)
	}
}
