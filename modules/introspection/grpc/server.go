package grpc

// Server exposes introspection gRPC surfaces.
type Server struct{}

// NewServer creates a new introspection gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
