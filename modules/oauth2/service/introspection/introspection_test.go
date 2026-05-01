package introspection

import (
	"context"
	"errors"
	"testing"

	"github.com/aegion/aegion/modules/oauth2/service/token"
)

type stubIntrospector struct {
	resp *token.IntrospectionResponse
	err  error
}

func (s *stubIntrospector) IntrospectToken(ctx context.Context, req *token.IntrospectionRequest) (*token.IntrospectionResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestService_IntrospectToken(t *testing.T) {
	t.Run("delegates success response", func(t *testing.T) {
		svc := NewService(&stubIntrospector{resp: &token.IntrospectionResponse{Active: true}})
		resp, err := svc.IntrospectToken(context.Background(), &token.IntrospectionRequest{Token: "token"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp == nil || !resp.Active {
			t.Fatalf("expected active response, got %#v", resp)
		}
	})

	t.Run("propagates upstream errors", func(t *testing.T) {
		svc := NewService(&stubIntrospector{err: errors.New("boom")})
		_, err := svc.IntrospectToken(context.Background(), &token.IntrospectionRequest{Token: "token"})
		if err == nil || err.Error() != "boom" {
			t.Fatalf("expected boom error, got %v", err)
		}
	})
}
