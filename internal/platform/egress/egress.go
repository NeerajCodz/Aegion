// Package egress validates and executes outbound HTTP requests against an
// operator-owned destination policy.
package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

var (
	ErrDestinationNotAllowed = errors.New("egress destination is not allowlisted")
	ErrUnsafeAddress         = errors.New("egress destination resolves to a forbidden address")
	ErrRedirectNotAllowed    = errors.New("egress redirects are not allowed")
	ErrResponseTooLarge      = errors.New("egress response exceeds size limit")
)

// Resolver is deliberately narrow so destination validation can be tested and
// every dial can resolve again to defend against DNS rebinding.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Policy is an immutable operator-supplied outbound destination policy.
type Policy struct {
	AllowedHosts []string
	AllowedCIDRs []string
	TrustedCIDRs []string
	Timeout      time.Duration
	MaxBodyBytes int64
	Resolver     Resolver
}

// Client validates each request URL and revalidates DNS addresses immediately
// before dialing. It does not follow redirects.
type Client struct {
	policy      compiledPolicy
	httpClient  *http.Client
	maxBodySize int64
}

type compiledPolicy struct {
	allowedHosts []string
	allowedCIDRs []netip.Prefix
	trustedCIDRs []netip.Prefix
	resolver     Resolver
}

// NewClient compiles the supplied operator policy. At least one host or CIDR
// allowlist entry is required, because unrestricted egress is never safe.
func NewClient(policy Policy) (*Client, error) {
	compiled, err := compilePolicy(policy)
	if err != nil {
		return nil, err
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	maxBodyBytes := policy.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 1 << 20
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DialContext = compiled.dialContext

	return &Client{
		policy: compiled,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return ErrRedirectNotAllowed
			},
		},
		maxBodySize: maxBodyBytes,
	}, nil
}

// ValidateURL parses and validates one HTTPS destination without performing an
// HTTP request. It rejects credentials and validates every DNS answer.
func (c *Client) ValidateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse egress URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("invalid egress URL: %w", ErrDestinationNotAllowed)
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("egress URL must not contain a fragment")
	}
	if _, err := c.policy.resolveAndValidate(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

// Do validates the request destination and caps the response body. Callers
// must still close the returned body.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("egress request URL is required")
	}
	if _, err := c.ValidateURL(req.Context(), req.URL.String()); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirectNotAllowed) {
			return nil, ErrRedirectNotAllowed
		}
		return nil, err
	}
	if resp.ContentLength > c.maxBodySize {
		_ = resp.Body.Close()
		return nil, ErrResponseTooLarge
	}
	resp.Body = &limitedReadCloser{ReadCloser: resp.Body, remaining: c.maxBodySize}
	return resp, nil
}

func compilePolicy(policy Policy) (compiledPolicy, error) {
	allowedHosts := make([]string, 0, len(policy.AllowedHosts))
	for _, host := range policy.AllowedHosts {
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
		if host == "" || strings.Contains(host, "://") || strings.Contains(host, "/") {
			return compiledPolicy{}, fmt.Errorf("invalid allowed host %q", host)
		}
		allowedHosts = append(allowedHosts, host)
	}
	allowedCIDRs, err := parsePrefixes(policy.AllowedCIDRs)
	if err != nil {
		return compiledPolicy{}, fmt.Errorf("invalid allowed CIDR: %w", err)
	}
	if len(allowedHosts) == 0 && len(allowedCIDRs) == 0 {
		return compiledPolicy{}, errors.New("at least one egress host or CIDR allowlist entry is required")
	}
	trustedCIDRs, err := parsePrefixes(policy.TrustedCIDRs)
	if err != nil {
		return compiledPolicy{}, fmt.Errorf("invalid trusted CIDR: %w", err)
	}
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return compiledPolicy{
		allowedHosts: allowedHosts,
		allowedCIDRs: allowedCIDRs,
		trustedCIDRs: trustedCIDRs,
		resolver:     resolver,
	}, nil
}

func parsePrefixes(raw []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (p compiledPolicy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := p.resolveAndValidate(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialErr error
	for _, address := range addresses {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	return nil, dialErr
}

func (p compiledPolicy) resolveAndValidate(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || !p.hostAllowed(host) {
		return nil, ErrDestinationNotAllowed
	}

	if literal, err := netip.ParseAddr(host); err == nil {
		if err := p.validateAddress(literal.Unmap()); err != nil {
			return nil, err
		}
		return []netip.Addr{literal.Unmap()}, nil
	}

	addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve egress host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve egress host %q: no addresses", host)
	}
	resolved := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if err := p.validateAddress(address); err != nil {
			return nil, err
		}
		resolved = append(resolved, address)
	}
	return slices.Compact(resolved), nil
}

func (p compiledPolicy) hostAllowed(host string) bool {
	if literal, err := netip.ParseAddr(host); err == nil {
		return inPrefixes(literal.Unmap(), p.allowedCIDRs)
	}
	for _, allowed := range p.allowedHosts {
		if host == allowed || strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, strings.TrimPrefix(allowed, "*")) {
			return true
		}
	}
	return false
}

func (p compiledPolicy) validateAddress(address netip.Addr) error {
	if !address.IsValid() || isForbiddenAddress(address) {
		if inPrefixes(address, p.trustedCIDRs) && inPrefixes(address, p.allowedCIDRs) {
			return nil
		}
		return ErrUnsafeAddress
	}
	if len(p.allowedCIDRs) != 0 && !inPrefixes(address, p.allowedCIDRs) && len(p.allowedHosts) == 0 {
		return ErrDestinationNotAllowed
	}
	return nil
}

func inPrefixes(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func isForbiddenAddress(address netip.Addr) bool {
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var forbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

type limitedReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *limitedReadCloser) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var one [1]byte
		n, err := r.ReadCloser.Read(one[:])
		if n > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.ReadCloser.Read(p)
	if int64(n) > r.remaining {
		allowed := int(r.remaining)
		r.remaining = 0
		return allowed, ErrResponseTooLarge
	}
	r.remaining -= int64(n)
	return n, err
}
