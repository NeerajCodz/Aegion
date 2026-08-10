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
	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

type ssoRepoStub struct {
	listConnectionsFn     func(ctx context.Context, includeDisabled bool) ([]store.Connection, error)
	getConnectionSlugFn   func(ctx context.Context, slug string) (*store.Connection, error)
	getConnectionDomainFn func(ctx context.Context, domain string) (*store.Connection, error)
	upsertConnectionFn    func(ctx context.Context, connection store.Connection) (*store.Connection, error)
	deleteConnectionFn    func(ctx context.Context, slug string) error
	createAuthRequestFn   func(ctx context.Context, requestID, connectionSlug string, expiresAt time.Time) error
	consumeAuthRequestFn  func(ctx context.Context, requestID, connectionSlug string, now time.Time) (bool, error)
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

func (s *ssoRepoStub) CreateAuthRequest(ctx context.Context, requestID, connectionSlug string, expiresAt time.Time) error {
	if s.createAuthRequestFn != nil {
		return s.createAuthRequestFn(ctx, requestID, connectionSlug, expiresAt)
	}
	return nil
}

func (s *ssoRepoStub) ConsumeAuthRequest(ctx context.Context, requestID, connectionSlug string, now time.Time) (bool, error) {
	if s.consumeAuthRequestFn != nil {
		return s.consumeAuthRequestFn(ctx, requestID, connectionSlug, now)
	}
	return strings.TrimSpace(requestID) != "", nil
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

func mustEncodeSignedSAMLResponse(t *testing.T, signer *samlTestSigner, requestID, issuer string, attributes map[string]string, includeNameID bool, statusCode string) string {
	t.Helper()
	if strings.TrimSpace(issuer) == "" {
		issuer = "urn:test:idp"
	}
	if strings.TrimSpace(statusCode) == "" {
		statusCode = "urn:oasis:names:tc:SAML:2.0:status:Success"
	}
	now := time.Now().UTC()

	doc := etree.NewDocument()
	response := doc.CreateElement("Response")
	response.CreateAttr("ID", "_response-id")
	response.CreateAttr("InResponseTo", requestID)
	response.CreateAttr("IssueInstant", now.Format(time.RFC3339))
	response.CreateElement("Issuer").SetText(issuer)
	status := response.CreateElement("Status")
	status.CreateElement("StatusCode").CreateAttr("Value", statusCode)

	assertion := response.CreateElement("Assertion")
	assertion.CreateAttr("ID", "_assertion-id")
	assertion.CreateElement("Issuer").SetText(issuer)
	subject := assertion.CreateElement("Subject")
	if includeNameID {
		subject.CreateElement("NameID").SetText(strings.TrimSpace(attributes["subject"]))
	}
	confirmation := subject.CreateElement("SubjectConfirmation")
	confirmationData := confirmation.CreateElement("SubjectConfirmationData")
	confirmationData.CreateAttr("InResponseTo", requestID)
	confirmationData.CreateAttr("NotOnOrAfter", now.Add(5*time.Minute).Format(time.RFC3339))

	conditions := assertion.CreateElement("Conditions")
	conditions.CreateAttr("NotBefore", now.Add(-1*time.Minute).Format(time.RFC3339))
	conditions.CreateAttr("NotOnOrAfter", now.Add(5*time.Minute).Format(time.RFC3339))

	attributeStatement := assertion.CreateElement("AttributeStatement")
	for key, value := range attributes {
		attribute := attributeStatement.CreateElement("Attribute")
		attribute.CreateAttr("Name", key)
		attribute.CreateElement("AttributeValue").SetText(value)
	}

	signingContext, err := dsig.NewSigningContext(signer.privateKey, [][]byte{signer.certificateDER})
	if err != nil {
		t.Fatalf("create signing context: %v", err)
	}
	root, err := signingContext.SignEnveloped(response)
	if err != nil {
		t.Fatalf("sign saml response: %v", err)
	}
	doc.SetRoot(root)

	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize saml response: %v", err)
	}
	return base64.StdEncoding.EncodeToString(xmlBytes)
}

func TestServiceAdditionalStartAndCompleteErrorBranches(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	ctx := context.Background()

	t.Run("new with nil repo does not create a memory fallback", func(t *testing.T) {
		svc := New(nil, secret)
		if svc.repo != nil {
			t.Fatal("expected nil repository to remain unavailable")
		}
		if _, err := svc.ListConnections(ctx); !errors.Is(err, ErrRepositoryUnavailable) {
			t.Fatalf("ListConnections(nil repository) error = %v", err)
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

		svc = New(&ssoRepoStub{
			getConnectionSlugFn: func(context.Context, string) (*store.Connection, error) {
				return &store.Connection{Slug: "acme", Enabled: true, SSOURL: "https://idp.example.com/sso"}, nil
			},
			createAuthRequestFn: func(context.Context, string, string, time.Time) error {
				return boom
			},
		}, secret)
		if _, err := svc.StartAuth(ctx, "acme", "/after"); !errors.Is(err, boom) {
			t.Fatalf("StartAuth(persist request error) = %v", err)
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
		signer := newSAMLTestSigner(t)
		connection := &store.Connection{
			Slug:           "acme",
			Enabled:        true,
			DisplayName:    "Acme",
			EntityID:       "urn:test:idp",
			SSOURL:         "https://idp.example.com/sso",
			CertificatePEM: signer.certificatePEM,
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
			"_saml_response": mustEncodeSignedSAMLResponse(t, signer, "_req-map", "urn:test:idp", map[string]string{
				"sub_attr":  "subject-from-map",
				"mail_attr": "USER@EXAMPLE.COM",
				"name_attr": "  User Name  ",
			}, false, ""),
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
			"_saml_response": mustEncodeSignedSAMLResponse(t, signer, "_req-map-missing", "urn:test:idp", map[string]string{
				"mail_attr": "user@example.com",
			}, false, ""),
		}); !errors.Is(err, ErrInvalidSAMLResponse) {
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
		ctx := context.Background()
		repo := store.New()
		if err := repo.CreateAuthRequest(ctx, "request-1", "acme", now.Add(authRequestTTL)); err != nil {
			t.Fatalf("CreateAuthRequest() error = %v", err)
		}
		consumed, err := repo.ConsumeAuthRequest(ctx, "request-1", "acme", now)
		if err != nil || !consumed {
			t.Fatalf("ConsumeAuthRequest(first) consumed=%t err=%v", consumed, err)
		}
		consumed, err = repo.ConsumeAuthRequest(ctx, "request-1", "acme", now.Add(time.Second))
		if err != nil || consumed {
			t.Fatalf("ConsumeAuthRequest(replay) consumed=%t err=%v", consumed, err)
		}
		consumed, err = repo.ConsumeAuthRequest(ctx, " ", "acme", now)
		if err != nil || consumed {
			t.Fatalf("ConsumeAuthRequest(blank) consumed=%t err=%v", consumed, err)
		}
	})
}

func TestServiceAdditionalParseAndValidationHelpers(t *testing.T) {
	now := time.Now().UTC()

	t.Run("parse saml response decode and validation branches", func(t *testing.T) {
		signer := newSAMLTestSigner(t)
		conn := &store.Connection{EntityID: "urn:test:idp", CertificatePEM: signer.certificatePEM}

		if _, err := parseSAMLResponse("%%%bad%%%", nil, "_req", nil, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(invalid base64) = %v", err)
		}

		if _, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte("<Response>")), conn, "_req", nil, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(xml unmarshal error) = %v", err)
		}

		nonSuccess := mustEncodeSignedSAMLResponse(t, signer, "_req", "urn:test:idp", map[string]string{
			"subject": "sub",
		}, true, "urn:oasis:names:tc:SAML:2.0:status:Responder")
		if _, err := parseSAMLResponse(nonSuccess, conn, "_req", nil, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(non-success status) = %v", err)
		}

		rawStdSigned := mustEncodeSignedSAMLResponse(t, signer, "_req", "urn:test:idp", map[string]string{
			"subject": "subject-raw",
		}, true, "")
		rawStdXML, err := base64.StdEncoding.DecodeString(rawStdSigned)
		if err != nil {
			t.Fatalf("DecodeString(raw std fixture) = %v", err)
		}
		parsed, err := parseSAMLResponse(base64.RawStdEncoding.EncodeToString(rawStdXML), conn, "_req", nil, now)
		if err != nil || parsed.Subject != "subject-raw" {
			t.Fatalf("parseSAMLResponse(raw std base64) parsed=%+v err=%v", parsed, err)
		}

		if _, err := parseSAMLResponse(rawStdSigned, conn, "_other", nil, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(request mismatch) = %v", err)
		}
	})

	t.Run("parse saml response attribute fallback and missing subject", func(t *testing.T) {
		signer := newSAMLTestSigner(t)
		conn := &store.Connection{
			EntityID:       "urn:test:idp",
			CertificatePEM: signer.certificatePEM,
			AttributeMapping: store.AttributeMapping{
				Subject:     "subject_attr",
				Email:       "email_attr",
				DisplayName: "display_attr",
			},
		}
		xmlWithAttrs := mustEncodeSignedSAMLResponse(t, signer, "_req-map", "urn:test:idp", map[string]string{
			"subject_attr": "sub-attr",
			"email_attr":   "user@example.com",
			"display_attr": "User Name",
			"empty_value":  " ",
		}, false, "")
		parsed, err := parseSAMLResponse(xmlWithAttrs, conn, "_req-map", nil, now)
		if err != nil {
			t.Fatalf("parseSAMLResponse(attribute fallback) = %v", err)
		}
		if parsed.Subject != "sub-attr" || parsed.Email != "user@example.com" || parsed.DisplayName != "User Name" {
			t.Fatalf("parseSAMLResponse(attribute fallback) unexpected parsed value: %+v", parsed)
		}
		if _, ok := parsed.Attributes["empty_value"]; ok {
			t.Fatalf("parseSAMLResponse(attribute fallback) should skip empty attribute values")
		}

		xmlNoSubject := mustEncodeSignedSAMLResponse(t, signer, "_req-none", "urn:test:idp", map[string]string{
			"email_attr": "user@example.com",
		}, false, "")
		if _, err := parseSAMLResponse(xmlNoSubject, conn, "_req-none", nil, now); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseSAMLResponse(missing subject) = %v", err)
		}
	})

	t.Run("certificate parsing and signature verification branches", func(t *testing.T) {
		if _, err := parseCertificatesPEM("-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----"); err == nil {
			t.Fatalf("parseCertificatesPEM(invalid cert bytes) expected error")
		}
		if _, err := parseCertificatesPEM("-----BEGIN NOTCERT-----\nYWJj\n-----END NOTCERT-----"); !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("parseCertificatesPEM(non-cert blocks) = %v", err)
		}

		signer := newSAMLTestSigner(t)
		conn := &store.Connection{CertificatePEM: signer.certificatePEM}
		if _, _, err := verifySAMLSignature([]byte("<"), conn); err == nil {
			t.Fatalf("verifySAMLSignature(invalid xml bytes) expected error")
		}
		if _, _, err := verifySAMLSignature([]byte(`<Response><Issuer>urn:test:idp</Issuer></Response>`), conn); !errors.Is(err, ErrInvalidSAMLResponse) {
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
		if matchesRecipients([]string{"https://sp.example.com/cb"}, "") {
			t.Fatalf("matchesRecipients(empty values) expected false when recipients are required")
		}
		if !matchesRecipients([]string{"https://sp.example.com/cb"}, "https://sp.example.com/cb") {
			t.Fatalf("matchesRecipients(exact recipient) expected true")
		}
		if normalizeRecipient(" http://[::1") != "http://[::1" {
			t.Fatalf("normalizeRecipient(parse error) should preserve original text")
		}
		if normalizeRecipient("ftp://sp.example.com/cb") != "ftp://sp.example.com/cb" {
			t.Fatalf("normalizeRecipient(unsupported scheme) should preserve full url")
		}
		if _, ok := parseSAMLTimestamp("invalid"); ok {
			t.Fatalf("parseSAMLTimestamp(invalid) expected false")
		}

		if !matchesRecipients([]string{"https://sp.example.com/cb"}, "https://SP.EXAMPLE.COM/cb#fragment") {
			t.Fatalf("matchesRecipients(normalized url) expected true")
		}
	})
}
