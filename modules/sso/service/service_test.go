package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/sso/store"
	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

func TestStartAndCompleteAuth(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	signer := newSAMLTestSigner(t)

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:              "acme",
		DisplayName:       "Acme",
		EntityID:          "urn:test:idp",
		SSOURL:            "https://idp.example.com/sso",
		CertificatePEM:    signer.certificatePEM,
		AttributeMapping:  store.AttributeMapping{Subject: "subject", Email: "email", DisplayName: "display_name"},
		Domains:           []string{"example.com"},
		DefaultRedirectTo: "/welcome",
		Enabled:           true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	if !strings.Contains(start.RedirectURL, "RelayState=") {
		t.Fatalf("expected relay state in redirect URL, got %q", start.RedirectURL)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}

	result, err := svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": signer.mustEncodeResponse(t, samlResponseOptions{
			requestID:   state.RequestID,
			issuer:      "urn:test:idp",
			destination: "/self-service/sso/acme/callback",
			recipient:   "/self-service/sso/acme/callback",
			subject:     "sub-123",
			email:       "user@example.com",
			displayName: "User",
			signed:      true,
		}),
		"_expected_recipients": []string{"http://example.com/self-service/sso/acme/callback", "/self-service/sso/acme/callback"},
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if result.Subject != "sub-123" || result.Email != "user@example.com" || result.DisplayName != "User" || result.RedirectTo != "/after" {
		t.Fatalf("unexpected callback result: %+v", result)
	}
}

func TestGetConnectionForDomain(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:acme:test",
		SSOURL:      "https://idp.example.com/sso",
		Domains:     []string{"example.com"},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	connection, err := svc.GetConnectionForDomain(ctx, "@example.com")
	if err != nil {
		t.Fatalf("get connection by domain: %v", err)
	}
	if connection.Slug != "acme" {
		t.Fatalf("expected acme connection, got %+v", connection)
	}
}

func TestNormalizeRedirect(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to root", in: "", want: "/"},
		{name: "relative path allowed", in: "/after-login", want: "/after-login"},
		{name: "relative path with query allowed", in: "/after-login?from=sso", want: "/after-login?from=sso"},
		{name: "absolute URL blocked", in: "https://attacker.example/callback", want: "/"},
		{name: "protocol relative blocked", in: "//attacker.example/callback", want: "/"},
		{name: "plain token blocked", in: "after-login", want: "/"},
		{name: "newlines blocked", in: "/ok\r\nSet-Cookie:bad=1", want: "/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRedirect(tc.in); got != tc.want {
				t.Fatalf("normalizeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUpsertConnectionHydratesMetadata(t *testing.T) {
	metadata := `<?xml version="1.0"?>
<EntityDescriptor entityID="urn:test:idp">
  <IDPSSODescriptor>
    <KeyDescriptor>
      <KeyInfo>
        <X509Data>
          <X509Certificate>MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtestcert</X509Certificate>
        </X509Data>
      </KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/saml"/>
  </IDPSSODescriptor>
</EntityDescriptor>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(metadata))
	}))
	defer ts.Close()

	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	connection, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "meta",
		DisplayName: "Meta",
		MetadataURL: ts.URL,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("upsert from metadata: %v", err)
	}
	if connection.EntityID != "urn:test:idp" {
		t.Fatalf("expected hydrated entity id, got %q", connection.EntityID)
	}
	if connection.SSOURL != "https://idp.example.com/saml" {
		t.Fatalf("expected hydrated sso url, got %q", connection.SSOURL)
	}
	if !strings.Contains(connection.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Fatalf("expected hydrated certificate, got %q", connection.CertificatePEM)
	}
}

func TestCompleteAuthRejectsUnsignedSAMLResponse(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	signer := newSAMLTestSigner(t)

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:           "acme",
		DisplayName:    "Acme",
		EntityID:       "urn:test:idp",
		SSOURL:         "https://idp.example.com/sso",
		CertificatePEM: signer.certificatePEM,
		AttributeMapping: store.AttributeMapping{
			Subject:     "subject",
			Email:       "email",
			DisplayName: "display_name",
		},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": signer.mustEncodeResponse(t, samlResponseOptions{
			requestID:   state.RequestID,
			issuer:      "urn:test:idp",
			destination: "/self-service/sso/acme/callback",
			recipient:   "/self-service/sso/acme/callback",
			subject:     "sub-xml",
			email:       "xml@example.com",
			displayName: "XML User",
			signed:      false,
		}),
		"_expected_recipients": []string{"http://example.com/self-service/sso/acme/callback", "/self-service/sso/acme/callback"},
	})
	if err == nil {
		t.Fatal("expected unsigned SAMLResponse to be rejected")
	}
}

func TestCompleteAuthRejectsMismatchedIssuer(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	signer := newSAMLTestSigner(t)
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:           "acme",
		DisplayName:    "Acme",
		EntityID:       "urn:test:idp",
		SSOURL:         "https://idp.example.com/sso",
		CertificatePEM: signer.certificatePEM,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": signer.mustEncodeResponse(t, samlResponseOptions{
			requestID:   state.RequestID,
			issuer:      "urn:wrong:idp",
			destination: "/self-service/sso/acme/callback",
			recipient:   "/self-service/sso/acme/callback",
			subject:     "sub-xml",
			signed:      true,
		}),
		"_expected_recipients": []string{"http://example.com/self-service/sso/acme/callback", "/self-service/sso/acme/callback"},
	})
	if err == nil {
		t.Fatal("expected mismatched issuer to fail")
	}
}

func TestCompleteAuthRejectsExpiredAssertion(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	signer := newSAMLTestSigner(t)
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:           "acme",
		DisplayName:    "Acme",
		EntityID:       "urn:test:idp",
		SSOURL:         "https://idp.example.com/sso",
		CertificatePEM: signer.certificatePEM,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": signer.mustEncodeResponse(t, samlResponseOptions{
			requestID:    state.RequestID,
			issuer:       "urn:test:idp",
			destination:  "/self-service/sso/acme/callback",
			recipient:    "/self-service/sso/acme/callback",
			subject:      "sub-xml",
			notBefore:    time.Now().UTC().Add(-30 * time.Minute),
			notOnOrAfter: time.Now().UTC().Add(-10 * time.Minute),
			signed:       true,
		}),
		"_expected_recipients": []string{"http://example.com/self-service/sso/acme/callback", "/self-service/sso/acme/callback"},
	})
	if err == nil {
		t.Fatal("expected expired assertion to fail")
	}
}

func TestCompleteAuthRejectsMismatchedRecipient(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	signer := newSAMLTestSigner(t)
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:           "acme",
		DisplayName:    "Acme",
		EntityID:       "urn:test:idp",
		SSOURL:         "https://idp.example.com/sso",
		CertificatePEM: signer.certificatePEM,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	start, err := svc.StartAuth(ctx, "acme", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": signer.mustEncodeResponse(t, samlResponseOptions{
			requestID:   state.RequestID,
			issuer:      "urn:test:idp",
			destination: "/self-service/sso/other/callback",
			recipient:   "/self-service/sso/other/callback",
			subject:     "sub-xml",
			signed:      true,
		}),
		"_expected_recipients": []string{"http://example.com/self-service/sso/acme/callback", "/self-service/sso/acme/callback"},
	})
	if err == nil {
		t.Fatal("expected mismatched recipient to fail")
	}
}

type samlResponseOptions struct {
	requestID    string
	issuer       string
	destination  string
	recipient    string
	subject      string
	email        string
	displayName  string
	notBefore    time.Time
	notOnOrAfter time.Time
	signed       bool
}

type samlTestSigner struct {
	privateKey     *rsa.PrivateKey
	certificateDER []byte
	certificatePEM string
}

func newSAMLTestSigner(t *testing.T) *samlTestSigner {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UTC().UnixNano()),
		Subject:      pkix.Name{CommonName: "aegion-sso-test"},
		NotBefore:    time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return &samlTestSigner{
		privateKey:     privateKey,
		certificateDER: der,
		certificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
	}
}

func (s *samlTestSigner) mustEncodeResponse(t *testing.T, opts samlResponseOptions) string {
	t.Helper()

	if strings.TrimSpace(opts.issuer) == "" {
		opts.issuer = "urn:test:idp"
	}
	if strings.TrimSpace(opts.subject) == "" {
		opts.subject = "sub-xml"
	}
	if opts.notBefore.IsZero() {
		opts.notBefore = time.Now().UTC().Add(-1 * time.Minute)
	}
	if opts.notOnOrAfter.IsZero() {
		opts.notOnOrAfter = time.Now().UTC().Add(5 * time.Minute)
	}

	doc := etree.NewDocument()
	response := doc.CreateElement("Response")
	response.CreateAttr("ID", "_response-id")
	response.CreateAttr("InResponseTo", opts.requestID)
	response.CreateAttr("IssueInstant", time.Now().UTC().Format(time.RFC3339))
	if strings.TrimSpace(opts.destination) != "" {
		response.CreateAttr("Destination", strings.TrimSpace(opts.destination))
	}
	response.CreateElement("Issuer").SetText(opts.issuer)
	status := response.CreateElement("Status")
	status.CreateElement("StatusCode").CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")

	assertion := response.CreateElement("Assertion")
	assertion.CreateAttr("ID", "_assertion-id")
	assertion.CreateElement("Issuer").SetText(opts.issuer)
	subject := assertion.CreateElement("Subject")
	subject.CreateElement("NameID").SetText(opts.subject)
	confirmation := subject.CreateElement("SubjectConfirmation")
	confirmationData := confirmation.CreateElement("SubjectConfirmationData")
	confirmationData.CreateAttr("InResponseTo", opts.requestID)
	confirmationData.CreateAttr("NotOnOrAfter", opts.notOnOrAfter.UTC().Format(time.RFC3339))
	if strings.TrimSpace(opts.recipient) != "" {
		confirmationData.CreateAttr("Recipient", strings.TrimSpace(opts.recipient))
	}
	conditions := assertion.CreateElement("Conditions")
	conditions.CreateAttr("NotBefore", opts.notBefore.UTC().Format(time.RFC3339))
	conditions.CreateAttr("NotOnOrAfter", opts.notOnOrAfter.UTC().Format(time.RFC3339))
	attributeStatement := assertion.CreateElement("AttributeStatement")
	if strings.TrimSpace(opts.email) != "" {
		attribute := attributeStatement.CreateElement("Attribute")
		attribute.CreateAttr("Name", "email")
		attribute.CreateElement("AttributeValue").SetText(strings.TrimSpace(opts.email))
	}
	if strings.TrimSpace(opts.displayName) != "" {
		attribute := attributeStatement.CreateElement("Attribute")
		attribute.CreateAttr("Name", "display_name")
		attribute.CreateElement("AttributeValue").SetText(strings.TrimSpace(opts.displayName))
	}
	attribute := attributeStatement.CreateElement("Attribute")
	attribute.CreateAttr("Name", "subject")
	attribute.CreateElement("AttributeValue").SetText(strings.TrimSpace(opts.subject))

	var root *etree.Element
	if opts.signed {
		signingContext, err := dsig.NewSigningContext(s.privateKey, [][]byte{s.certificateDER})
		if err != nil {
			t.Fatalf("create signing context: %v", err)
		}
		root, err = signingContext.SignEnveloped(response)
		if err != nil {
			t.Fatalf("sign saml response: %v", err)
		}
	} else {
		root = response
	}
	doc.SetRoot(root)
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize saml response: %v", err)
	}
	return base64.StdEncoding.EncodeToString(xmlBytes)
}
