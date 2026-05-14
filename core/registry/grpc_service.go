package registry

import (
	"context"
	"strings"
	"time"

	pb "github.com/aegion/aegion/internal/proto/core"
)

// GRPCService exposes the module registry over the internal gRPC control plane.
type GRPCService struct {
	pb.UnimplementedModuleRegistryServer
	registry *Registry
}

// NewGRPCService creates a gRPC registry service.
func NewGRPCService(registry *Registry) *GRPCService {
	return &GRPCService{registry: registry}
}

func (s *GRPCService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req == nil || s.registry == nil {
		return &pb.RegisterResponse{Success: false, Error: "invalid registration request"}, nil
	}
	module := strings.TrimSpace(req.GetModule())
	address := strings.TrimSpace(req.GetAddress())
	if module == "" || address == "" {
		return &pb.RegisterResponse{Success: false, Error: "module and address are required"}, nil
	}
	id := module
	if existing, err := s.registry.GetModule(id); err == nil && existing != nil {
		id = module + "-" + time.Now().UTC().Format("20060102150405")
	}
	resp, err := s.registry.Register(RegistrationRequest{
		ID:      id,
		Name:    module,
		Version: req.GetVersion(),
		Endpoints: []Endpoint{{
			Type: EndpointGRPC,
			URL:  "grpc://" + address,
		}},
		HealthURL: "",
		Metadata: map[string]string{
			"routes":              strings.Join(req.GetRoutes(), ","),
			"capabilities":        strings.Join(req.GetCapabilities(), ","),
			"grpc_services":       strings.Join(req.GetGrpcServices(), ","),
			"event_subscriptions": strings.Join(req.GetEventSubscriptions(), ","),
		},
	})
	if err != nil {
		return &pb.RegisterResponse{Success: false, Error: err.Error()}, nil
	}
	return &pb.RegisterResponse{InstanceId: resp.ModuleID, Success: true}, nil
}

func (s *GRPCService) Deregister(ctx context.Context, req *pb.DeregisterRequest) (*pb.DeregisterResponse, error) {
	if req == nil || s.registry == nil {
		return &pb.DeregisterResponse{Success: false}, nil
	}
	_, err := s.registry.Deregister(req.GetInstanceId())
	return &pb.DeregisterResponse{Success: err == nil}, nil
}

func (s *GRPCService) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if req == nil || s.registry == nil {
		return &pb.HeartbeatResponse{Accepted: false, ShouldReregister: true}, nil
	}
	status := StatusHealthy
	switch req.GetStatus() {
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		status = StatusUnhealthy
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		status = StatusStarting
	}
	if err := s.registry.UpdateStatus(req.GetInstanceId(), status); err != nil {
		return &pb.HeartbeatResponse{Accepted: false, ShouldReregister: true}, nil
	}
	return &pb.HeartbeatResponse{Accepted: true}, nil
}

func (s *GRPCService) GetModules(ctx context.Context, req *pb.GetModulesRequest) (*pb.GetModulesResponse, error) {
	if s.registry == nil {
		return &pb.GetModulesResponse{}, nil
	}
	var query *ModuleQuery
	if req != nil && req.GetModuleFilter() != "" {
		query = &ModuleQuery{Name: req.GetModuleFilter()}
	}
	modules := s.registry.ListModules(query)
	out := make([]*pb.ModuleInstance, 0, len(modules))
	for _, module := range modules {
		if req != nil && !req.GetIncludeUnhealthy() && module.Status == StatusUnhealthy {
			continue
		}
		out = append(out, &pb.ModuleInstance{
			InstanceId:    module.ID,
			Module:        module.Name,
			Version:       module.Version,
			Address:       firstGRPCAddress(module.Endpoints),
			Routes:        splitMetadata(module.Metadata["routes"]),
			Capabilities:  splitMetadata(module.Metadata["capabilities"]),
			Status:        protoHealthStatus(module.Status),
			RegisteredAt:  module.RegisteredAt.Unix(),
			LastHeartbeat: module.LastHealthAt.Unix(),
		})
	}
	return &pb.GetModulesResponse{Modules: out}, nil
}

func firstGRPCAddress(endpoints []Endpoint) string {
	for _, endpoint := range endpoints {
		if endpoint.Type == EndpointGRPC {
			return strings.TrimPrefix(endpoint.URL, "grpc://")
		}
	}
	return ""
}

func splitMetadata(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func protoHealthStatus(status ModuleStatus) pb.HealthStatus {
	switch status {
	case StatusHealthy:
		return pb.HealthStatus_HEALTH_STATUS_HEALTHY
	case StatusUnhealthy:
		return pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	case StatusStarting:
		return pb.HealthStatus_HEALTH_STATUS_DEGRADED
	default:
		return pb.HealthStatus_HEALTH_STATUS_UNKNOWN
	}
}
