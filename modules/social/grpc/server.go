package grpc

// Server exposes social gRPC surfaces.
type Server struct{}

// NewServer creates a new social gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
