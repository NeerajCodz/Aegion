package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestCompleteAuthFromSAMLResponse(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:acme:test",
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
	xmlResponse := `<Response><Issuer>urn:test:idp</Issuer><Assertion><Subject><NameID>sub-xml</NameID></Subject><AttributeStatement><Attribute Name="email"><AttributeValue>xml@example.com</AttributeValue></Attribute><Attribute Name="display_name"><AttributeValue>XML User</AttributeValue></Attribute></AttributeStatement></Assertion></Response>`
	result, err := svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err != nil {
		t.Fatalf("complete auth from saml response: %v", err)
	}
	if result.Subject != "sub-xml" || result.Email != "xml@example.com" || result.DisplayName != "XML User" {
		t.Fatalf("unexpected callback result: %+v", result)
	}
}
