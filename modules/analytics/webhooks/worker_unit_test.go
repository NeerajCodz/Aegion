package webhooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type noopLogger struct{}

func (noopLogger) Debug(string, ...interface{}) {}
func (noopLogger) Info(string, ...interface{})  {}
func (noopLogger) Warn(string, ...interface{})  {}
func (noopLogger) Error(string, ...interface{}) {}

func TestDeliveryWorker_processJob_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	var (
		mu                sync.Mutex
		saveDeliveryCalls int
		resetFailureCalls int
	)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			require.True(t, strings.Contains(query, "FROM webhooks"))
			return fakeRow{scanFn: func(dest ...interface{}) error {
				now := time.Now().UTC()
				*(dest[0].(*string)) = "wh_1"
				*(dest[1].(*string)) = "user_1"
				*(dest[2].(*string)) = srv.URL
				*(dest[3].(*string)) = `["evt.a"]`
				*(dest[4].(*string)) = `[]`
				*(dest[5].(*string)) = `{}`
				*(dest[6].(*string)) = "secret"
				*(dest[7].(*bool)) = true
				*(dest[8].(*int)) = 0
				*(dest[9].(*time.Time)) = now
				*(dest[10].(*time.Time)) = now
				return nil
			}}
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			mu.Lock()
			defer mu.Unlock()
			switch {
			case strings.Contains(query, "INSERT INTO webhook_deliveries"):
				saveDeliveryCalls++
			case strings.Contains(query, "UPDATE webhooks SET failure_count = 0"):
				resetFailureCalls++
			}
			return fakeResult{affected: 1}, nil
		},
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			return &fakeRows{data: [][]interface{}{}}, nil
		},
	}

	store := NewStore(db)
	queue := NewQueue(10)
	dispatcher := NewDispatcher(2, noopLogger{})
	retry := NewRetryPolicy(RetryConfig{MaxRetries: 5, BackoffBaseMs: 0, CircuitBreakerThreshold: 10})
	worker := NewDeliveryWorker(1, queue, store, dispatcher, retry, NewSignature(), noopLogger{})

	job := &DeliveryJob{
		ID:         "job_1",
		WebhookID:  "wh_1",
		EventID:    "evt_1",
		Payload:    map[string]interface{}{"hello": "world"},
		Headers:    map[string]string{"X-Test": "1"},
		Attempts:   0,
		MaxRetries: 2,
	}

	worker.processJob(context.Background(), job)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, saveDeliveryCalls)
	require.Equal(t, 1, resetFailureCalls)
	require.Equal(t, 0, queue.Pending())
}

func TestDeliveryWorker_processJob_RetryPath_Requeues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var (
		mu                sync.Mutex
		saveDeliveryCalls int
	)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error {
				now := time.Now().UTC()
				*(dest[0].(*string)) = "wh_1"
				*(dest[1].(*string)) = "user_1"
				*(dest[2].(*string)) = srv.URL
				*(dest[3].(*string)) = `["evt.a"]`
				*(dest[4].(*string)) = `[]`
				*(dest[5].(*string)) = `{}`
				*(dest[6].(*string)) = "secret"
				*(dest[7].(*bool)) = true
				*(dest[8].(*int)) = 0
				*(dest[9].(*time.Time)) = now
				*(dest[10].(*time.Time)) = now
				return nil
			}}
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(query, "INSERT INTO webhook_deliveries") {
				saveDeliveryCalls++
			}
			return fakeResult{affected: 1}, nil
		},
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			return &fakeRows{data: [][]interface{}{}}, nil
		},
	}

	store := NewStore(db)
	queue := NewQueue(10)
	dispatcher := NewDispatcher(2, noopLogger{})
	retry := NewRetryPolicy(RetryConfig{MaxRetries: 5, BackoffBaseMs: 0, CircuitBreakerThreshold: 10})
	worker := NewDeliveryWorker(1, queue, store, dispatcher, retry, NewSignature(), noopLogger{})

	job := &DeliveryJob{
		ID:         "job_1",
		WebhookID:  "wh_1",
		EventID:    "evt_1",
		Payload:    map[string]interface{}{"hello": "world"},
		Headers:    map[string]string{"X-Test": "1"},
		Attempts:   0,
		MaxRetries: 2,
	}

	worker.processJob(context.Background(), job)

	mu.Lock()
	require.Equal(t, 1, saveDeliveryCalls)
	mu.Unlock()

	require.Equal(t, 1, queue.Pending())
	requeued := queue.Dequeue()
	require.NotNil(t, requeued)
	require.NotNil(t, requeued.NextRetryAt)
}

func TestDeliveryWorker_processJob_DisabledMovesToDLQ(t *testing.T) {
	var (
		mu       sync.Mutex
		dlqCalls int
	)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error {
				now := time.Now().UTC()
				*(dest[0].(*string)) = "wh_1"
				*(dest[1].(*string)) = "user_1"
				*(dest[2].(*string)) = "https://example.invalid"
				*(dest[3].(*string)) = `[]`
				*(dest[4].(*string)) = `[]`
				*(dest[5].(*string)) = `{}`
				*(dest[6].(*string)) = "secret"
				*(dest[7].(*bool)) = false // disabled
				*(dest[8].(*int)) = 0
				*(dest[9].(*time.Time)) = now
				*(dest[10].(*time.Time)) = now
				return nil
			}}
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(query, "INSERT INTO webhook_dlq") {
				dlqCalls++
			}
			return fakeResult{affected: 1}, nil
		},
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			return &fakeRows{data: [][]interface{}{}}, nil
		},
	}

	store := NewStore(db)
	queue := NewQueue(10)
	dispatcher := NewDispatcher(2, noopLogger{})
	retry := NewRetryPolicy(RetryConfig{MaxRetries: 5, BackoffBaseMs: 0, CircuitBreakerThreshold: 1})
	worker := NewDeliveryWorker(1, queue, store, dispatcher, retry, NewSignature(), noopLogger{})

	job := &DeliveryJob{
		ID:         "job_1",
		WebhookID:  "wh_1",
		EventID:    "evt_1",
		Payload:    map[string]interface{}{"hello": "world"},
		Headers:    map[string]string{"X-Test": "1"},
		Attempts:   0,
		MaxRetries: 2,
	}

	worker.processJob(context.Background(), job)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, dlqCalls)
	require.Equal(t, 0, queue.Pending())
}

func TestDeliveryWorker_processJob_CircuitBreakFinalFailureMovesToDLQ(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var (
		mu       sync.Mutex
		dlqCalls int
		incCalls int
	)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error {
				now := time.Now().UTC()
				*(dest[0].(*string)) = "wh_1"
				*(dest[1].(*string)) = "user_1"
				*(dest[2].(*string)) = srv.URL
				*(dest[3].(*string)) = `[]`
				*(dest[4].(*string)) = `[]`
				*(dest[5].(*string)) = `{}`
				*(dest[6].(*string)) = "secret"
				*(dest[7].(*bool)) = true
				*(dest[8].(*int)) = 0
				*(dest[9].(*time.Time)) = now
				*(dest[10].(*time.Time)) = now
				return nil
			}}
		},
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			mu.Lock()
			defer mu.Unlock()
			switch {
			case strings.Contains(query, "INSERT INTO webhook_dlq"):
				dlqCalls++
			case strings.Contains(query, "UPDATE webhooks SET failure_count = failure_count + 1"):
				incCalls++
			}
			return fakeResult{affected: 1}, nil
		},
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			return &fakeRows{data: [][]interface{}{}}, nil
		},
	}

	store := NewStore(db)
	queue := NewQueue(10)
	dispatcher := NewDispatcher(2, noopLogger{})
	// Circuit breaker trips at 1 failure; job has max retries 0 so it is treated as final failure.
	retry := NewRetryPolicy(RetryConfig{MaxRetries: 5, BackoffBaseMs: 0, CircuitBreakerThreshold: 1})
	worker := NewDeliveryWorker(1, queue, store, dispatcher, retry, NewSignature(), noopLogger{})

	job := &DeliveryJob{
		ID:         "job_1",
		WebhookID:  "wh_1",
		EventID:    "evt_1",
		Payload:    map[string]interface{}{"hello": "world"},
		Headers:    map[string]string{"X-Test": "1"},
		Attempts:   0,
		MaxRetries: 0,
	}

	worker.processJob(context.Background(), job)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, incCalls)
	require.Equal(t, 1, dlqCalls)
}
