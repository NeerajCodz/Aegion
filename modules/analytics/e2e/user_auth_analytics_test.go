package e2e

import (
	"os"
	"testing"
)

func TestUserAuthAnalyticsEndToEndContract(t *testing.T) {
	if os.Getenv("AEGION_E2E") == "" {
		t.Skip("set AEGION_E2E=1 and provide a full end-to-end environment with auth, analytics, and admin surfaces wired together")
	}
	t.Skip("e2e harness not implemented in this repo yet")
}
