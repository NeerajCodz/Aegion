package grpc

// Server exposes SSO gRPC surfaces.
type Server struct{}

// NewServer creates a new SSO gRPC server adapter.
func NewServer() *Server {
	return &Server{}
}
