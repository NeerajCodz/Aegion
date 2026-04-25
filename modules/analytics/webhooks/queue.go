package webhooks

import (
	"sync"
	"time"
)

// DeliveryJob represents a single webhook delivery job.
type DeliveryJob struct {
	ID          string
	WebhookID   string
	EventID     string
	EventType   string
	Category    string
	Payload     interface{}
	Headers     map[string]string
	CreatedAt   time.Time
	Attempts    int
	MaxRetries  int
	NextRetryAt *time.Time
}

// Queue manages a queue of webhook delivery jobs.
type Queue struct {
	jobs     chan *DeliveryJob
	maxSize  int
	mu       sync.RWMutex
	closed   bool
	pending  map[string]*DeliveryJob
}

// NewQueue creates a new delivery queue.
func NewQueue(maxSize int) *Queue {
	if maxSize <= 0 {
		maxSize = 1000
	}

	return &Queue{
		jobs:    make(chan *DeliveryJob, maxSize),
		maxSize: maxSize,
		pending: make(map[string]*DeliveryJob),
	}
}

// Enqueue adds a job to the queue.
func (q *Queue) Enqueue(job *DeliveryJob) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return ErrQueueClosed
	}
	q.mu.Unlock()

	select {
	case q.jobs <- job:
		q.mu.Lock()
		q.pending[job.ID] = job
		q.mu.Unlock()
		return nil
	default:
		return ErrQueueFull
	}
}

// Dequeue retrieves a job from the queue.
func (q *Queue) Dequeue() *DeliveryJob {
	select {
	case job := <-q.jobs:
		return job
	default:
		return nil
	}
}

// DequeueTimeout retrieves a job with a timeout.
func (q *Queue) DequeueTimeout(timeout time.Duration) *DeliveryJob {
	select {
	case job := <-q.jobs:
		return job
	case <-time.After(timeout):
		return nil
	}
}

// Remove removes a job from pending tracking.
func (q *Queue) Remove(jobID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, jobID)
}

// Pending returns the number of pending jobs.
func (q *Queue) Pending() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.pending)
}

// Close closes the queue.
func (q *Queue) Close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return ErrQueueClosed
	}
	q.closed = true
	q.mu.Unlock()

	close(q.jobs)
	return nil
}

// Error definitions
var (
	ErrQueueFull   = &WebhookError{Code: "QUEUE_FULL", Message: "delivery queue is full"}
	ErrQueueClosed = &WebhookError{Code: "QUEUE_CLOSED", Message: "delivery queue is closed"}
)
