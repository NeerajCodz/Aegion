package eventbus

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	pb "github.com/aegion/aegion/internal/proto/core"
)

// GRPCService exposes the internal event bus over gRPC.
type GRPCService struct {
	pb.UnimplementedEventBusServiceServer
	bus *Bus
}

// NewGRPCService creates a gRPC event bus service.
func NewGRPCService(bus *Bus) *GRPCService {
	return &GRPCService{bus: bus}
}

func (s *GRPCService) Publish(ctx context.Context, req *pb.PublishRequest) (*pb.PublishResponse, error) {
	if req == nil || s.bus == nil {
		return &pb.PublishResponse{Success: false, Error: "invalid publish request"}, nil
	}
	payload := map[string]interface{}{}
	if len(req.GetPayload()) > 0 {
		if err := json.Unmarshal(req.GetPayload(), &payload); err != nil {
			return &pb.PublishResponse{Success: false, Error: "invalid payload JSON"}, nil
		}
	}
	metadata := make(map[string]interface{}, len(req.GetMetadata()))
	for k, v := range req.GetMetadata() {
		metadata[k] = v
	}
	event := Event{
		ID:           uuid.New(),
		Type:         req.GetEventType(),
		SourceModule: req.GetSourceModule(),
		EntityType:   req.GetEntityType(),
		EntityID:     req.GetEntityId(),
		Payload:      payload,
		Metadata:     metadata,
		OccurredAt:   time.Now().UTC(),
	}
	if err := s.bus.Publish(ctx, event); err != nil {
		return &pb.PublishResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.PublishResponse{EventId: event.ID.String(), Success: true}, nil
}

func (s *GRPCService) Subscribe(req *pb.SubscribeRequest, stream pb.EventBusService_SubscribeServer) error {
	if req == nil || s.bus == nil {
		return nil
	}
	sub := s.bus.Subscribe(req.GetSubscriber(), req.GetEventTypes(), func(ctx context.Context, event Event) error {
		payload, _ := json.Marshal(event.Payload)
		metadata := make(map[string]string, len(event.Metadata))
		for k, v := range event.Metadata {
			metadata[k] = toString(v)
		}
		identityID := ""
		if event.IdentityID != nil {
			identityID = event.IdentityID.String()
		}
		return stream.Send(&pb.Event{
			Id:           event.ID.String(),
			EventType:    event.Type,
			SourceModule: event.SourceModule,
			EntityType:   event.EntityType,
			EntityId:     event.EntityID,
			IdentityId:   identityID,
			Payload:      payload,
			Metadata:     metadata,
			OccurredAt:   event.OccurredAt.Unix(),
		})
	})
	defer s.bus.Unsubscribe(sub)
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (s *GRPCService) Acknowledge(ctx context.Context, req *pb.AcknowledgeRequest) (*pb.AcknowledgeResponse, error) {
	if req == nil || req.GetEventId() == "" {
		return &pb.AcknowledgeResponse{Success: false}, nil
	}
	if _, err := uuid.Parse(req.GetEventId()); err != nil {
		return &pb.AcknowledgeResponse{Success: false}, nil
	}
	return &pb.AcknowledgeResponse{Success: true}, nil
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
