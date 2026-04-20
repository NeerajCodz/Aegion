package service

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/sso/store"
)

type ssoRepoStub struct {
	listConnectionsFn    func(ctx context.Context, includeDisabled bool) ([]store.Connection, error)
	getConnectionSlugFn  func(ctx context.Context, slug string) (*store.Connection, error)
	getConnectionDomainFn func(ctx context.Context, domain string) (*store.Connection, error)
	upsertConnectionFn   func(ctx context.Context, connection store.Connection) (*store.Connection, error)
	deleteConnectionFn   func(ctx context.Context, slug string) error
}

func (s *ssoRepoStub) ListConnections(ctx context.Context, includeDisabled bool) ([]store.Connection, error) {
	if s.listConnectionsFn != nil {
		return s.listConnectionsFn(ctx, includeDisabled)
	}
	return []store.Connection{}, nil
}

func (s *ssoRepoStub) GetConnectionBySlug(ctx context.Context, slug string) (*store.Connection, error) {
	if s.getConnectionSlugFn != nil {
		return s.getConnectionSlugFn(ctx, slug)
	}
	return nil, store.ErrConnectionNotFound
}

func (s *ssoRepoStub) GetConnectionByDomain(ctx context.Context, domain string) (*store.Connection, error) {
	if s.getConnectionDomainFn != nil {
		return s.getConnectionDomainFn(ctx, domain)
	}
	return nil, store.ErrConnectionNotFound
}

func (s *ssoRepoStub) UpsertConnection(ctx context.Context, connection store.Connection) (*store.Connection, error) {
	if s.upsertConnectionFn != nil {
		return s.upsertConnectionFn(ctx, connection)
	}
	return &connection, nil
}

func (s *ssoRepoStub) DeleteConnection(ctx context.Context, slug string) error {
	if s.deleteConnectionFn != nil {
		return s.deleteConnectionFn(ctx, slug)
	}
	return nil
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type readErrCloser struct{}

func (readErrCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (readErrCloser) Close() error { return nil }

func TestServiceAdditionalStartAndCompleteErrorBranches(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	ctx := context.Background()

	t.Run("new with nil repo uses default memory store", func(t *testing.T) {
		svc := New(nil, secret)
		if svc.repo == nil {
			t.Fatal("expected default repository to be configured")
		}
		if _, err := svc.ListConnections(ctx); err != nil {
			t.Fatalf("ListConnections() error = %v", err)
		}
	})

	t.Run("start auth repository and validation errors", func(t *testing.T) {
		boom := errors.New("boom")
		svc := New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return nil, boom
			},
		}, secret)
		if _, err := svc.StartAuth(ctx, "acme", "/after"); !errors.Is(err, boom) {
			t.Fatalf("StartAuth(repo error) = %v", err)
		}

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: false}, nil
			},
		}, secret)
		if _, err := svc.StartAuth(ctx, "acme", "/after"); !errors.Is(err, ErrConnectionDisabled) {
			t.Fatalf("StartAuth(disabled) = %v", err)
		}

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: true, SSOURL: "https://idp.example.com/sso"}, nil
			},
		}, nil)
		if _, err := svc.StartAuth(ctx, "acme", "/after"); !errors.Is(err, ErrMissingStateSecret) {
			t.Fatalf("StartAuth(missing secret) = %v", err)
		}

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: true, SSOURL: "://bad"}, nil
			},
		}, secret)
		if _, err := svc.StartAuth(ctx, "acme", "/after"); err == nil {
			t.Fatalf("StartAuth(invalid SSO URL) expected error")
		}
	})

	t.Run("complete auth repository and relay-state errors", func(t *testing.T) {
		boom := errors.New("boom")
		svc := New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return nil, boom
			},
		}, secret)
		if _, err := svc.CompleteAuth(ctx, "acme", "state", "sub", "", "", nil); !errors.Is(err, boom) {
			t.Fatalf("CompleteAuth(repo error) = %v", err)
		}

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: false}, nil
			},
		}, secret)
		if _, err := svc.CompleteAuth(ctx, "acme", "state", "sub", "", "", nil); !errors.Is(err, ErrConnectionDisabled) {
			t.Fatalf("CompleteAuth(disabled) = %v", err)
		}

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: true}, nil
			},
		}, secret)
		if _, err := svc.CompleteAuth(ctx, "acme", "invalid", "sub", "", "", nil); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("CompleteAuth(invalid relay state) = %v", err)
		}

		mismatchRelay, err := svc.signRelayState(callbackState{
			Connection: "other",
			RequestID:  "_req-mismatch",
			RedirectTo: "/after",
			IssuedAt:   time.Now().UTC().Unix(),
		})
		if err != nil {
			t.Fatalf("signRelayState(mismatch): %v", err)
		}
		if _, err := svc.CompleteAuth(ctx, "acme", mismatchRelay, "sub", "", "", nil); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("CompleteAuth(connection mismatch) = %v", err)
		}

		blankReqRelay, err := svc.signRelayState(callbackState{
			Connection: "acme",
			RequestID:  " ",
			RedirectTo: "/after",
			IssuedAt:   time.Now().UTC().Unix(),
		})
		if err != nil {
			t.Fatalf("signRelayState(blank request): %v", err)
		}
		if _, err := svc.CompleteAuth(ctx, "acme", blankReqRelay, "sub", "", "", nil); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("CompleteAuth(blank request ID) = %v", err)
		}
	})

	t.Run("complete auth attribute mapping fallback and missing subject", func(t *testing.T) {
		connection := &store.Connection{
			Slug:        "acme",
			Enabled:     true,
			DisplayName: "Acme",
			EntityID:    "urn:test:idp",
			SSOURL:      "https://idp.example.com/sso",
			AttributeMapping: store.AttributeMapping{
				Subject:     "sub_attr",
				Email:       "mail_attr",
				DisplayName: "name_attr",
			},
		}
		svc := New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return connection, nil
			},
		}, secret)

		relay, err := svc.signRelayState(callbackState{
			Connection: "acme",
			RequestID:  "_req-map",
			RedirectTo: "/mapped",
			IssuedAt:   time.Now().UTC().Unix(),
		})
		if err != nil {
			t.Fatalf("signRelayState(mapping): %v", err)
		}
		got, err := svc.CompleteAuth(ctx, "acme", relay, "", "", "", map[string]interface{}{
			"sub_attr":  "subject-from-map",
			"mail_attr": "USER@EXAMPLE.COM",
			"name_attr": "  User Name  ",
		})
		if err != nil {
			t.Fatalf("CompleteAuth(mapping success): %v", err)
		}
		if got.Subject != "subject-from-map" || got.Email != "user@example.com" || got.DisplayName != "User Name" {
			t.Fatalf("CompleteAuth(mapping success) unexpected result: %+v", got)
		}

		relay, err = svc.signRelayState(callbackState{
			Connection: "acme",
			RequestID:  "_req-map-missing",
			IssuedAt:   time.Now().UTC().Unix(),
		})
		if err != nil {
			t.Fatalf("signRelayState(mapping missing): %v", err)
		}
		if _, err := svc.CompleteAuth(ctx, "acme", relay, "", "", "", map[string]interface{}{
			"mail_attr": "user@example.com",
		}); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("CompleteAuth(mapping missing subject) = %v", err)
		}
	})
}

func TestServiceAdditionalNormalizeAndMetadataBranches(t *testing.T) {
	ctx := context.Background()
	secret := []byte("01234567890123456789012345678901")

	t.Run("normalize connection rejects missing fields and invalid URL", func(t *testing.T) {
		svc := New(store.New(), secret)
		if _, err := svc.normalizeConnection(ctx, ConnectionUpsertRequest{
			Slug:        "acme",
			DisplayName: "Acme",
			EntityID:    "urn:test:idp",
		}); !errors.Is(err, store.ErrConnectionNotFound) {
			t.Fatalf("normalizeConnection(missing fields) = %v", err)
		}
		if _, err := svc.normalizeConnection(ctx, ConnectionUpsertRequest{
			Slug:        "acme",
			DisplayName: "Acme",
			EntityID:    "urn:test:idp",
			SSOURL:      "://bad",
		}); !errors.Is(err, store.ErrConnectionNotFound) {
			t.Fatalf("normalizeConnection(invalid URL) = %v", err)
		}
	})

	t.Run("normalize connection returns metadata fetch errors", func(t *testing.T) {
		svc := New(store.New(), secret)
		if _, err := svc.normalizeConnection(ctx, ConnectionUpsertRequest{
			Slug:        "acme",
			DisplayName: "Acme",
			MetadataURL: "http://[::1",
		}); err == nil {
			t.Fatalf("normalizeConnection(metadata fetch error) expected error")
		}
	})

	t.Run("fetch metadata request/build/read/xml error branches", func(t *testing.T) {
		svc := New(store.New(), secret)
		if _, err := svc.fetchMetadata(ctx, "http://[::1"); err == nil {
			t.Fatalf("fetchMetadata(invalid URL) expected error")
		}

		svc.httpClient = &http.Client{Timeout: 200 * time.Millisecond}
		if _, err := svc.fetchMetadata(ctx, "http://127.0.0.1:1/metadata"); err == nil {
			t.Fatalf("fetchMetadata(do error) expected error")
		}

		svc.httpClient = &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       io.NopCloser(strings.NewReader("upstream down")),
					Header:     make(http.Header),
				}, nil
			}),
		}
		if _, err := svc.fetchMetadata(ctx, "https://idp.example.com/metadata"); !errors.Is(err, ErrConnectionDisabled) {
			t.Fatalf("fetchMetadata(non-2xx) = %v", err)
		}

		svc.httpClient = &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       readErrCloser{},
					Header:     make(http.Header),
				}, nil
			}),
		}
		if _, err := svc.fetchMetadata(ctx, "https://idp.example.com/metadata"); err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("fetchMetadata(read error) = %v", err)
		}

		svc.httpClient = &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("<not-xml")),
					Header:     make(http.Header),
				}, nil
			}),
		}
		if _, err := svc.fetchMetadata(ctx, "https://idp.example.com/metadata"); err == nil {
			t.Fatalf("fetchMetadata(xml error) expected error")
		}
	})

	t.Run("build redirect and authn request helper branches", func(t *testing.T) {
		if _, err := buildRedirectURL(store.Connection{SSOURL: "://bad"}, "relay", "_req"); err == nil {
			t.Fatalf("buildRedirectURL(invalid URL) expected error")
		}
		authn, err := buildAuthnRequest(store.Connection{Slug: "acme", SSOURL: "https://idp.example.com/sso"}, "")
		if err != nil {
			t.Fatalf("buildAuthnRequest(empty requestID): %v", err)
		}
		if !strings.Contains(authn, `ID="_`) {
			t.Fatalf("buildAuthnRequest(expected generated ID), got %q", authn)
		}
	})
}

func TestServiceAdditionalRelayAndHelperBranches(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	svc := New(store.New(), secret)

	t.Run("verify relay rejects tampered and non-json payload", func(t *testing.T) {
		signed, err := svc.signRelayState(callbackState{
			Connection: "acme",
			RequestID:  "_req-verify",
			IssuedAt:   time.Now().UTC().Unix(),
		})
		if err != nil {
			t.Fatalf("signRelayState() error = %v", err)
		}
		tampered := signed[:len(signed)-1] + "x"
		if _, err := svc.verifyRelayState(tampered); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("verifyRelayState(tampered) = %v", err)
		}

		payload := []byte("not-json")
		envelope, err := platformcrypto.SignEnvelope(relayStateKind, secret, payload, time.Now().UTC())
		if err != nil {
			t.Fatalf("SignEnvelope() error = %v", err)
		}
		raw := base64.RawURLEncoding.EncodeToString(payload) + "." + envelope
		if _, err := svc.verifyRelayState(raw); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("verifyRelayState(non-json payload) = %v", err)
		}
	})

	t.Run("normalize domains and consume request branches", func(t *testing.T) {
		got := normalizeDomains([]string{"@Example.com", "example.com", " ", "@other.com", "@other.com"})
		if len(got) != 2 || got[0] != "example.com" || got[1] != "other.com" {
			t.Fatalf("normalizeDomains() = %#v", got)
		}

		now := time.Now().UTC()
		svc := New(store.New(), secret)
		svc.seenRequest["old-id"] = now.Add(-31 * time.Minute)
		if ok := svc.consumeRequestID("new-id", now); !ok {
			t.Fatalf("consumeRequestID(new-id) expected true")
		}
		if _, ok := svc.seenRequest["old-id"]; ok {
			t.Fatalf("consumeRequestID() should prune expired replay IDs")
		}
		if ok := svc.consumeRequestID("new-id", now.Add(time.Second)); ok {
			t.Fatalf("consumeRequestID(replay) expected false")
		}
		if ok := svc.consumeRequestID("   ", now); ok {
			t.Fatalf("consumeRequestID(blank) expected false")
		}

		var nilSvc *Service
		if ok := nilSvc.consumeRequestID("id", now); ok {
			t.Fatalf("consumeRequestID(nil receiver) expected false")
		}
	})
}

func TestServiceAdditionalParseAndValidationHelpers(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(5 * time.Minute).Format(time.RFC3339)

	t.Run("parse saml response decode and validation branches", func(t *testing.T) {
		if _, err := parseSAMLResponse("%%%bad%%%", nil, "_req", now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(invalid base64) = %v", err)
		}

		if _, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte("<Response>")), nil, "_req", now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(xml unmarshal error) = %v", err)
		}

		nonSuccess := `<Response InResponseTo="_req"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Responder"/></Status><Assertion><Subject><NameID>sub</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req" NotOnOrAfter="` + future + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + future + `"/></Assertion></Response>`
		if _, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(nonSuccess)), nil, "_req", now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(non-success status) = %v", err)
		}

		rawStd := `<Response InResponseTo="_req"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Subject><NameID>subject-raw</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req" NotOnOrAfter="` + future + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + future + `"/></Assertion></Response>`
		parsed, err := parseSAMLResponse(base64.RawStdEncoding.EncodeToString([]byte(rawStd)), nil, "_req", now)
		if err != nil || parsed.Subject != "subject-raw" {
			t.Fatalf("parseSAMLResponse(raw std base64) parsed=%+v err=%v", parsed, err)
		}

		if _, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(rawStd)), nil, "_other", now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(request mismatch) = %v", err)
		}
	})

	t.Run("parse saml response attribute fallback and missing subject", func(t *testing.T) {
		xmlWithAttrs := `<Response InResponseTo="_req-map"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Subject><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req-map" NotOnOrAfter="` + future + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + future + `"/><AttributeStatement><Attribute Name="subject_attr"><AttributeValue>sub-attr</AttributeValue></Attribute><Attribute Name="email_attr"><AttributeValue>user@example.com</AttributeValue></Attribute><Attribute Name="display_attr"><AttributeValue>User Name</AttributeValue></Attribute><Attribute Name=""><AttributeValue>skip</AttributeValue></Attribute><Attribute Name="empty_value"><AttributeValue> </AttributeValue></Attribute></AttributeStatement></Assertion></Response>`
		conn := &store.Connection{
			AttributeMapping: store.AttributeMapping{
				Subject:     "subject_attr",
				Email:       "email_attr",
				DisplayName: "display_attr",
			},
		}
		parsed, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(xmlWithAttrs)), conn, "_req-map", now)
		if err != nil {
			t.Fatalf("parseSAMLResponse(attribute fallback) = %v", err)
		}
		if parsed.Subject != "sub-attr" || parsed.Email != "user@example.com" || parsed.DisplayName != "User Name" {
			t.Fatalf("parseSAMLResponse(attribute fallback) unexpected parsed value: %+v", parsed)
		}
		if _, ok := parsed.Attributes[""]; ok {
			t.Fatalf("parseSAMLResponse(attribute fallback) should skip empty attribute names")
		}
		if _, ok := parsed.Attributes["empty_value"]; ok {
			t.Fatalf("parseSAMLResponse(attribute fallback) should skip empty attribute values")
		}

		xmlNoSubject := `<Response InResponseTo="_req-none"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Subject><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req-none" NotOnOrAfter="` + future + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + future + `"/><AttributeStatement><Attribute Name="email_attr"><AttributeValue>user@example.com</AttributeValue></Attribute></AttributeStatement></Assertion></Response>`
		if _, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(xmlNoSubject)), conn, "_req-none", now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(missing subject) = %v", err)
		}
	})

	t.Run("certificate parsing and signature verification branches", func(t *testing.T) {
		if _, err := parseCertificates("-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----"); err == nil {
			t.Fatalf("parseCertificates(invalid cert bytes) expected error")
		}
		if _, err := parseCertificates("-----BEGIN NOTCERT-----\nYWJj\n-----END NOTCERT-----"); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseCertificates(non-cert blocks) = %v", err)
		}

		_, certPEM := buildSignedResponse(t, "urn:test:idp", "_req-signature", "sub", "user@example.com", "User", now.Add(5*time.Minute))
		conn := &store.Connection{CertificatePEM: certPEM}
		if err := verifySAMLSignature([]byte("<"), conn, now); err == nil {
			t.Fatalf("verifySAMLSignature(invalid xml bytes) expected error")
		}
		if err := verifySAMLSignature([]byte(`<Response><Issuer>urn:test:idp</Issuer></Response>`), conn, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("verifySAMLSignature(no assertion) = %v", err)
		}
	})

	t.Run("validation helper branches", func(t *testing.T) {
		if !matchesRequestID("", "anything") {
			t.Fatalf("matchesRequestID(empty expected) expected true")
		}
		if matchesRequestID("_expected", "_other") {
			t.Fatalf("matchesRequestID(mismatch) expected false")
		}

		if !matchesIssuer(nil, "issuer") {
			t.Fatalf("matchesIssuer(nil connection) expected true")
		}
		if !matchesIssuer(&store.Connection{}, "issuer") {
			t.Fatalf("matchesIssuer(empty entity) expected true")
		}

		if validTimeWindow(now, now.Add(5*time.Minute).Format(time.RFC3339), "") {
			t.Fatalf("validTimeWindow(not-before in future) expected false")
		}
		if !validNotOnOrAfter(now, "not-a-time") {
			t.Fatalf("validNotOnOrAfter(invalid timestamp) expected true")
		}
		if !validRecipientAndDestination("https://sp.example.com/cb", "") {
			t.Fatalf("validRecipientAndDestination(recipient only) expected true")
		}
		if !validRecipientAndDestination("", "https://sp.example.com/cb") {
			t.Fatalf("validRecipientAndDestination(destination only) expected true")
		}
		if _, ok := parseAbsoluteURL("http://[::1"); ok {
			t.Fatalf("parseAbsoluteURL(parse error) expected false")
		}
		if _, ok := parseAbsoluteURL("ftp://sp.example.com/cb"); ok {
			t.Fatalf("parseAbsoluteURL(unsupported scheme) expected false")
		}
		if _, ok := parseSAMLTimestamp("invalid"); ok {
			t.Fatalf("parseSAMLTimestamp(invalid) expected false")
		}

		if !validAudience(&store.Connection{Slug: "acme"}, []struct {
			Audience []string `xml:"Audience"`
		}{
			{Audience: []string{"urn:aegion:sp:acme"}},
		}) {
			t.Fatalf("validAudience(match) expected true")
		}
	})
}

