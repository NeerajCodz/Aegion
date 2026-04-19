package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/sso/store"
)

func TestStartAndCompleteAuth(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:              "acme",
		DisplayName:       "Acme",
		EntityID:          "urn:acme:test",
		SSOURL:            "https://idp.example.com/sso",
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

	result, err := svc.CompleteAuth(ctx, "acme", start.RelayState, "sub-123", "user@example.com", "User", map[string]interface{}{"department": "eng"})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}
	if result.Subject != "sub-123" || result.Email != "user@example.com" || result.RedirectTo != "/after" {
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

func TestCompleteAuthRejectsRawSAMLResponse(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:test:idp",
		SSOURL:      "https://idp.example.com/sso",
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
	expires := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	xmlResponse := `<Response InResponseTo="` + state.RequestID + `"><Issuer>urn:test:idp</Issuer><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Issuer>urn:test:idp</Issuer><Subject><NameID>sub-xml</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="` + state.RequestID + `" NotOnOrAfter="` + expires + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + expires + `"/><AttributeStatement><Attribute Name="email"><AttributeValue>xml@example.com</AttributeValue></Attribute><Attribute Name="display_name"><AttributeValue>XML User</AttributeValue></Attribute></AttributeStatement></Assertion></Response>`
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err == nil {
		t.Fatal("expected raw SAMLResponse to be rejected")
	}
}

func TestCompleteAuthRejectsMismatchedIssuer(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:test:idp",
		SSOURL:      "https://idp.example.com/sso",
		Enabled:     true,
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
	expires := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	xmlResponse := `<Response InResponseTo="` + state.RequestID + `"><Issuer>urn:wrong:idp</Issuer><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Issuer>urn:wrong:idp</Issuer><Subject><NameID>sub-xml</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="` + state.RequestID + `" NotOnOrAfter="` + expires + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + expires + `"/></Assertion></Response>`
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err == nil {
		t.Fatal("expected mismatched issuer to fail")
	}
}

func TestCompleteAuthRejectsExpiredAssertion(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()
	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:test:idp",
		SSOURL:      "https://idp.example.com/sso",
		Enabled:     true,
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
	expired := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	xmlResponse := `<Response InResponseTo="` + state.RequestID + `"><Issuer>urn:test:idp</Issuer><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Issuer>urn:test:idp</Issuer><Subject><NameID>sub-xml</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="` + state.RequestID + `" NotOnOrAfter="` + expired + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + expired + `"/></Assertion></Response>`
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err == nil {
		t.Fatal("expected expired assertion to fail")
	}
}
