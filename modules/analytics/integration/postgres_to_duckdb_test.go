package integration

import (
	"os"
	"testing"
)

func TestPostgresToDuckDBIntegrationContract(t *testing.T) {
	if os.Getenv("AEGION_INTEGRATION") == "" {
		t.Skip("set AEGION_INTEGRATION=1 and provide a live Postgres + DuckDB integration environment")
	}
	t.Skip("integration harness not implemented in this repo yet")
}
