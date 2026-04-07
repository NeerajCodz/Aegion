package grpc

import (
	"context"
	"errors"
	"testing"

	mfapb "github.com/aegion/aegion/internal/proto/mfa/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStatusProvider struct {
	status     *mfapb.MFAStatusResponse
	statusErr  error
	factors    []*mfapb.Factor
	factorsErr error
	lastID     string
}

func (m *mockStatusProvider) GetStatus(ctx context.Context, identityID string) (*mfapb.MFAStatusResponse, error) {
	m.lastID = identityID
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return m.status, nil
}

func (m *mockStatusProvider) GetEnrolledFactors(ctx context.Context, identityID string) ([]*mfapb.Factor, error) {
	m.lastID = identityID
	if m.factorsErr != nil {
		return nil, m.factorsErr
	}
	return m.factors, nil
}

func TestServer_DefaultResponses(t *testing.T) {
	s := NewServer()

	status, err := s.GetStatus(context.Background(), &mfapb.MFAStatusRequest{IdentityId: "identity-1"})
	require.NoError(t, err)
	assert.False(t, status.GetMfaEnrolled())
	assert.Equal(t, "aal1", status.GetHighestAal())
	assert.Empty(t, status.GetEnrolledMethods())

	factors, err := s.GetEnrolledFactors(context.Background(), &mfapb.FactorListRequest{IdentityId: "identity-1"})
	require.NoError(t, err)
	assert.Empty(t, factors.GetFactors())
}

func TestServer_ProviderResponses(t *testing.T) {
	provider := &mockStatusProvider{
		status: &mfapb.MFAStatusResponse{
			MfaEnrolled:     true,
			HighestAal:      "aal2",
			EnrolledMethods: []string{"totp"},
		},
		factors: []*mfapb.Factor{
			{Id: "factor-1", Method: "totp", Verified: true},
		},
	}
	s := NewServer(provider)

	status, err := s.GetStatus(context.Background(), &mfapb.MFAStatusRequest{IdentityId: "identity-42"})
	require.NoError(t, err)
	assert.True(t, status.GetMfaEnrolled())
	assert.Equal(t, "aal2", status.GetHighestAal())
	assert.Equal(t, "identity-42", provider.lastID)

	factors, err := s.GetEnrolledFactors(context.Background(), &mfapb.FactorListRequest{IdentityId: "identity-42"})
	require.NoError(t, err)
	assert.Len(t, factors.GetFactors(), 1)
	assert.Equal(t, "totp", factors.GetFactors()[0].GetMethod())
}

func TestServer_ProviderErrors(t *testing.T) {
	s := NewServer(&mockStatusProvider{statusErr: errors.New("status failed")})
	_, err := s.GetStatus(context.Background(), &mfapb.MFAStatusRequest{IdentityId: "identity-1"})
	assert.ErrorContains(t, err, "status failed")

	s = NewServer(&mockStatusProvider{factorsErr: errors.New("factors failed")})
	_, err = s.GetEnrolledFactors(context.Background(), &mfapb.FactorListRequest{IdentityId: "identity-1"})
	assert.ErrorContains(t, err, "factors failed")
}
