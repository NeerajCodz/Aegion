package grpc

// Server exposes passkeys gRPC surfaces.
type Server struct{}

// NewServer creates a new passkeys gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
