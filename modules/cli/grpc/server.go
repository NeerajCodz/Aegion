package grpc

// Server exposes CLI gRPC surfaces.
type Server struct{}

// NewServer creates a new CLI gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
