package grpc

// Server exposes proxy gRPC surfaces.
type Server struct{}

// NewServer creates a new proxy gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
