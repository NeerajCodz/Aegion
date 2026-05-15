package xlog

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/aegion/aegion/internal/proto/analytics"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

// AnalyticsSinkConfig controls best-effort analytics delivery.
type AnalyticsSinkConfig struct {
	Client        pb.AnalyticsServiceClient
	Conn          *grpc.ClientConn
	QueueSize     int
	BatchSize     int
	FlushInterval time.Duration
	Timeout       time.Duration
}

// AnalyticsSink batches xlog events into the analytics module over gRPC.
type AnalyticsSink struct {
	client        pb.AnalyticsServiceClient
	queue         chan Record
	batchSize     int
	flushInterval time.Duration
	timeout       time.Duration
	dropped       atomic.Int64
	closed        chan struct{}
	once          sync.Once
}

// NewAnalyticsSink creates and starts a best-effort async analytics sink.
func NewAnalyticsSink(cfg AnalyticsSinkConfig) *AnalyticsSink {
	client := cfg.Client
	if client == nil && cfg.Conn != nil {
		client = pb.NewAnalyticsServiceClient(cfg.Conn)
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	s := &AnalyticsSink{
		client:        client,
		queue:         make(chan Record, cfg.QueueSize),
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		timeout:       cfg.Timeout,
		closed:        make(chan struct{}),
	}
	go s.run()
	return s
}

// Emit enqueues without blocking the caller.
func (s *AnalyticsSink) Emit(_ context.Context, r Record) error {
	if s == nil || s.client == nil {
		return nil
	}
	select {
	case s.queue <- r:
		return nil
	default:
		s.dropped.Add(1)
		return nil
	}
}

// Dropped returns the number of records dropped because the queue was full.
func (s *AnalyticsSink) Dropped() int64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// Close stops the sink.
func (s *AnalyticsSink) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.closed) })
}

func (s *AnalyticsSink) run() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	batch := make([]Record, 0, s.batchSize)
	for {
		select {
		case <-s.closed:
			s.flush(batch)
			return
		case r := <-s.queue:
			batch = append(batch, r)
			if len(batch) >= s.batchSize {
				s.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (s *AnalyticsSink) flush(records []Record) {
	if len(records) == 0 || s.client == nil {
		return
	}
	events := make([]*pb.XLogEvent, 0, len(records))
	for _, record := range records {
		event, err := recordToProto(record)
		if err != nil {
			s.dropped.Add(1)
			continue
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if _, err := s.client.IngestXLogEvents(ctx, &pb.IngestXLogEventsRequest{Events: events}); err != nil {
		s.dropped.Add(int64(len(events)))
	}
}

func recordToProto(r Record) (*pb.XLogEvent, error) {
	fields, err := structpb.NewStruct(toStringKeyMap(r.Fields))
	if err != nil {
		return nil, err
	}
	return &pb.XLogEvent{
		EventId:        stringField(r.Fields, "event.id"),
		EventName:      stringField(r.Fields, "event.name"),
		EventKind:      stringField(r.Fields, "event.kind"),
		EventOutcome:   stringField(r.Fields, "event.outcome"),
		ServiceName:    stringField(r.Fields, "service.name"),
		ServiceVersion: stringField(r.Fields, "service.version"),
		Environment:    stringField(r.Fields, "environment"),
		RequestId:      stringField(r.Fields, "request.id"),
		TraceId:        stringField(r.Fields, "trace.id"),
		UserId:         stringField(r.Fields, "user.id"),
		SessionId:      stringField(r.Fields, "session.id"),
		Fields:         fields,
		DurationMs:     int64Field(r.Fields, "duration_ms"),
		Timestamp:      stringField(r.Fields, "timestamp"),
	}, nil
}

func toStringKeyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringField(fields map[string]any, key string) string {
	if v, ok := fields[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func int64Field(fields map[string]any, key string) int64 {
	switch v := fields[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}
