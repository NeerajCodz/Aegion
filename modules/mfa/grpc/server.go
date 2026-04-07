package grpc

// Server exposes MFA gRPC surfaces.
type Server struct{}

// NewServer creates a new MFA gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
