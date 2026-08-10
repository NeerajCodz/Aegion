package egress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func TestNewClientRequiresExplicitDestinationPolicy(t *testing.T) {
	_, err := NewClient(Policy{})
	require.ErrorContains(t, err, "allowlist")
}

func TestClientValidateURL(t *testing.T) {
	resolver := staticResolver{
		"idp.example.com":      {netip.MustParseAddr("1.1.1.1")},
		"loopback.example.com": {netip.MustParseAddr("127.0.0.1")},
		"private.example.com":  {netip.MustParseAddr("10.1.2.3")},
	}
	client, err := NewClient(Policy{
		AllowedHosts: []string{"*.example.com"},
		Resolver:     resolver,
	})
	require.NoError(t, err)

	valid, err := client.ValidateURL(context.Background(), "https://idp.example.com/.well-known/openid-configuration")
	require.NoError(t, err)
	require.Equal(t, "idp.example.com", valid.Hostname())

	for _, rawURL := range []string{
		"http://idp.example.com/metadata",
		"https://user:secret@idp.example.com/metadata",
		"https://outside.example.net/metadata",
		"https://loopback.example.com/metadata",
		"https://private.example.com/metadata",
	} {
		_, err := client.ValidateURL(context.Background(), rawURL)
		require.Error(t, err, rawURL)
	}
}

func TestClientValidateURLAllowsExplicitTrustedPrivateCIDR(t *testing.T) {
	client, err := NewClient(Policy{
		AllowedHosts: []string{"idp.internal.example"},
		AllowedCIDRs: []string{"10.0.0.0/8"},
		TrustedCIDRs: []string{"10.0.0.0/8"},
		Resolver: staticResolver{
			"idp.internal.example": {netip.MustParseAddr("10.1.2.3")},
		},
	})
	require.NoError(t, err)
	_, err = client.ValidateURL(context.Background(), "https://idp.internal.example/metadata")
	require.NoError(t, err)
}

func TestClientValidateURLRejectsUntrustedPrivateLiteral(t *testing.T) {
	client, err := NewClient(Policy{AllowedCIDRs: []string{"10.0.0.0/8"}})
	require.NoError(t, err)
	_, err = client.ValidateURL(context.Background(), "https://10.1.2.3/metadata")
	require.ErrorIs(t, err, ErrUnsafeAddress)
}

func TestLimitedReadCloserRejectsBodyOverLimit(t *testing.T) {
	body := &limitedReadCloser{
		ReadCloser: io.NopCloser(bytes.NewBufferString("abcdef")),
		remaining:  5,
	}
	contents, err := io.ReadAll(body)
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.Equal(t, "abcde", string(contents))
}

func TestLimitedReadCloserAllowsBodyAtLimit(t *testing.T) {
	body := &limitedReadCloser{
		ReadCloser: io.NopCloser(bytes.NewBufferString("abcde")),
		remaining:  5,
	}
	contents, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "abcde", string(contents))
}
