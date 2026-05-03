package performance_test

import (
	"testing"

	"github.com/aegion/aegion/modules/analytics/performance"
)

func TestBenchmarkSuite_QueryPerformance(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	// Run individual benchmarks
	suite.BenchmarkQuerySingleEvent(t)
	suite.BenchmarkQuery100Events(t)
	suite.BenchmarkQuery1000EventsWithFilter(t)

	// Verify baseline was met
	if suite.GetPassRate() < 80.0 {
		t.Fatalf("Query performance below acceptable threshold: %.1f%% pass rate", suite.GetPassRate())
	}
}

func TestBenchmarkSuite_AggregationPerformance(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	suite.BenchmarkAggregation1MEvents(t)
	suite.BenchmarkExport10KEvents(t)

	if suite.GetPassRate() < 80.0 {
		t.Fatalf("Aggregation performance below acceptable threshold: %.1f%% pass rate", suite.GetPassRate())
	}
}

func TestBenchmarkSuite_SyncPerformance(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	suite.BenchmarkRealtimeSync(t)
	suite.BenchmarkBatchSync100KEvents(t)

	if suite.GetPassRate() < 80.0 {
		t.Fatalf("Sync performance below acceptable threshold: %.1f%% pass rate", suite.GetPassRate())
	}
}

func TestBenchmarkSuite_DashboardPerformance(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	suite.BenchmarkDashboardLoad(t)

	if suite.GetPassRate() < 80.0 {
		t.Fatalf("Dashboard performance below acceptable threshold: %.1f%% pass rate", suite.GetPassRate())
	}
}

func TestBenchmarkSuite_ConcurrencyPerformance(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	suite.BenchmarkConcurrentQueries(t)
	suite.BenchmarkMemoryUsage(t)

	if suite.GetPassRate() < 80.0 {
		t.Fatalf("Concurrency performance below acceptable threshold: %.1f%% pass rate", suite.GetPassRate())
	}
}

func TestBenchmarkSuite_FullSuite(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	// Run all benchmarks
	suite.RunAllBenchmarks(t)

	// Verify all pass
	ok, msg := suite.VerifyBaselines()
	if !ok {
		t.Fatalf("Performance baselines not met: %s", msg)
	}

	t.Logf("Performance verification passed: %s", msg)
}

func TestBenchmarkSuite_PassRate(t *testing.T) {
	suite := performance.NewBenchmarkSuite()

	suite.RunAllBenchmarks(t)

	passRate := suite.GetPassRate()
	if passRate < 80.0 {
		t.Fatalf("Pass rate %.1f%% below 80%% threshold", passRate)
	}
}

// BenchmarkQuery100Events benchmarks querying 100 events
func BenchmarkQuery100Events(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkQuery100Events(b)
	}
}

// BenchmarkQuery1000EventsWithFilter benchmarks querying 1000 events with filter
func BenchmarkQuery1000EventsWithFilter(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkQuery1000EventsWithFilter(b)
	}
}

// BenchmarkAggregation1MEvents benchmarks aggregating 1M events
func BenchmarkAggregation1MEvents(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkAggregation1MEvents(b)
	}
}

// BenchmarkExport10KEvents benchmarks exporting 10K events
func BenchmarkExport10KEvents(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkExport10KEvents(b)
	}
}

// BenchmarkBatchSync100KEvents benchmarks batch syncing 100K events
func BenchmarkBatchSync100KEvents(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkBatchSync100KEvents(b)
	}
}

// BenchmarkDashboardLoad benchmarks dashboard load time
func BenchmarkDashboardLoad(b *testing.B) {
	suite := performance.NewBenchmarkSuite()

	for i := 0; i < b.N; i++ {
		suite.BenchmarkDashboardLoad(b)
	}
}
