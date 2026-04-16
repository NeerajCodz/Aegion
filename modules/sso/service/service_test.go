package service

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestCompleteAuthRejectsRelayStateReplay(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "acme",
		DisplayName: "Acme",
		EntityID:    "urn:acme:test",
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

	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "sub-123", "user@example.com", "User", nil)
	if err != nil {
		t.Fatalf("first complete auth failed: %v", err)
	}

	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "sub-123", "user@example.com", "User", nil)
	if err == nil {
		t.Fatal("expected replayed relay state to fail")
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

func TestCompleteAuthRejectsMismatchedRecipientAndDestination(t *testing.T) {
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
	xmlResponse := `<Response InResponseTo="` + state.RequestID + `" Destination="https://sp.example.com/callback"><Issuer>urn:test:idp</Issuer><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Issuer>urn:test:idp</Issuer><Subject><NameID>sub-xml</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="` + state.RequestID + `" Recipient="https://other.example.com/callback" NotOnOrAfter="` + expires + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + expires + `"/></Assertion></Response>`
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err == nil {
		t.Fatal("expected recipient/destination mismatch to fail")
	}
}

func TestCompleteAuthRejectsMismatchedAudience(t *testing.T) {
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
	xmlResponse := `<Response InResponseTo="` + state.RequestID + `"><Issuer>urn:test:idp</Issuer><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Issuer>urn:test:idp</Issuer><Subject><NameID>sub-xml</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="` + state.RequestID + `" NotOnOrAfter="` + expires + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + expires + `"><AudienceRestriction><Audience>urn:aegion:sp:other</Audience></AudienceRestriction></Conditions></Assertion></Response>`
	_, err = svc.CompleteAuth(ctx, "acme", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(xmlResponse)),
	})
	if err == nil {
		t.Fatal("expected mismatched audience to fail")
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

func TestCompleteAuthValidatesXMLSignature(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	startConn, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "signed",
		DisplayName: "Signed",
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
	start, err := svc.StartAuth(ctx, "signed", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	rawXML, certPEM := buildSignedResponse(t, "urn:test:idp", state.RequestID, "sub-signed", "signed@example.com", "Signed User", time.Now().UTC().Add(5*time.Minute))
	_, err = svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:             "signed",
		DisplayName:      "Signed",
		EntityID:         "urn:test:idp",
		SSOURL:           "https://idp.example.com/sso",
		CertificatePEM:   certPEM,
		AttributeMapping: startConn.AttributeMapping,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert signed connection: %v", err)
	}
	result, err := svc.CompleteAuth(ctx, "signed", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(rawXML)),
	})
	if err != nil {
		t.Fatalf("complete auth with signed xml: %v", err)
	}
	if result.Subject != "sub-signed" || result.Email != "signed@example.com" {
		t.Fatalf("unexpected signed callback result: %+v", result)
	}
}

func TestCompleteAuthRejectsTamperedSignedXML(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "signed",
		DisplayName: "Signed",
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
	start, err := svc.StartAuth(ctx, "signed", "/after")
	if err != nil {
		t.Fatalf("start auth: %v", err)
	}
	state, err := svc.verifyRelayState(start.RelayState)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	rawXML, certPEM := buildSignedResponse(t, "urn:test:idp", state.RequestID, "sub-signed", "signed@example.com", "Signed User", time.Now().UTC().Add(5*time.Minute))
	_, err = svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:           "signed",
		DisplayName:    "Signed",
		EntityID:       "urn:test:idp",
		SSOURL:         "https://idp.example.com/sso",
		CertificatePEM: certPEM,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("upsert signed connection: %v", err)
	}
	tampered := strings.Replace(rawXML, "signed@example.com", "attacker@example.com", 1)
	_, err = svc.CompleteAuth(ctx, "signed", start.RelayState, "", "", "", map[string]interface{}{
		"_saml_response": base64.StdEncoding.EncodeToString([]byte(tampered)),
	})
	if err == nil {
		t.Fatal("expected tampered signed xml to fail")
	}
}

func buildSignedResponse(t *testing.T, issuer, requestID, subject, email, displayName string, expires time.Time) (string, string) {
	t.Helper()
	keyStore := dsig.RandomKeyStoreForTest()
	privateKey, certDER, err := keyStore.GetKeyPair()
	if err != nil {
		t.Fatalf("get keypair: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	ctx := dsig.NewDefaultSigningContext(&staticKeyStore{privateKey: privateKey, cert: certDER})

	response := etree.NewElement("Response")
	response.CreateAttr("ID", "_resp123")
	response.CreateAttr("InResponseTo", requestID)
	response.CreateAttr("IssueInstant", time.Now().UTC().Format(time.RFC3339))
	issuerEl := response.CreateElement("Issuer")
	issuerEl.SetText(issuer)
	status := response.CreateElement("Status")
	statusCode := status.CreateElement("StatusCode")
	statusCode.CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")
	assertion := response.CreateElement("Assertion")
	assertion.CreateAttr("ID", "_assert123")
	assertionIssuer := assertion.CreateElement("Issuer")
	assertionIssuer.SetText(issuer)
	subjectEl := assertion.CreateElement("Subject")
	nameID := subjectEl.CreateElement("NameID")
	nameID.SetText(subject)
	subjectConfirmation := subjectEl.CreateElement("SubjectConfirmation")
	subjectConfirmationData := subjectConfirmation.CreateElement("SubjectConfirmationData")
	subjectConfirmationData.CreateAttr("InResponseTo", requestID)
	subjectConfirmationData.CreateAttr("NotOnOrAfter", expires.Format(time.RFC3339))
	conditions := assertion.CreateElement("Conditions")
	conditions.CreateAttr("NotBefore", time.Now().UTC().Add(-1*time.Minute).Format(time.RFC3339))
	conditions.CreateAttr("NotOnOrAfter", expires.Format(time.RFC3339))
	attrStmt := assertion.CreateElement("AttributeStatement")
	emailAttr := attrStmt.CreateElement("Attribute")
	emailAttr.CreateAttr("Name", "email")
	emailVal := emailAttr.CreateElement("AttributeValue")
	emailVal.SetText(email)
	nameAttr := attrStmt.CreateElement("Attribute")
	nameAttr.CreateAttr("Name", "display_name")
	nameVal := nameAttr.CreateElement("AttributeValue")
	nameVal.SetText(displayName)

	signed, err := ctx.SignEnveloped(response)
	if err != nil {
		t.Fatalf("sign response: %v", err)
	}
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	rawXML, err := doc.WriteToString()
	if err != nil {
		t.Fatalf("serialize signed response: %v", err)
	}
	return rawXML, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

type staticKeyStore struct {
	privateKey *rsa.PrivateKey
	cert       []byte
}

func (s *staticKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) {
	return s.privateKey, s.cert, nil
}

func TestConnectionCRUDWrappers(t *testing.T) {
	repo := store.New()
	svc := New(repo, []byte("01234567890123456789012345678901"))
	ctx := context.Background()

	_, err := svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "enabled",
		DisplayName: "Enabled",
		EntityID:    "urn:test:enabled",
		SSOURL:      "https://idp.example.com/sso",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("upsert enabled connection: %v", err)
	}
	_, err = svc.UpsertConnection(ctx, ConnectionUpsertRequest{
		Slug:        "disabled",
		DisplayName: "Disabled",
		EntityID:    "urn:test:disabled",
		SSOURL:      "https://idp.example.com/sso",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("upsert disabled connection: %v", err)
	}

	connections, err := svc.ListConnections(ctx)
	if err != nil {
		t.Fatalf("list enabled connections: %v", err)
	}
	if len(connections) != 1 || connections[0].Slug != "enabled" {
		t.Fatalf("unexpected enabled connections: %+v", connections)
	}

	allConnections, err := svc.ListConfiguredConnections(ctx, true)
	if err != nil {
		t.Fatalf("list configured connections: %v", err)
	}
	if len(allConnections) != 2 {
		t.Fatalf("expected two configured connections, got %d", len(allConnections))
	}

	connection, err := svc.GetConnection(ctx, "disabled")
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if connection.Slug != "disabled" {
		t.Fatalf("unexpected connection: %+v", connection)
	}

	if err := svc.DeleteConnection(ctx, "disabled"); err != nil {
		t.Fatalf("delete connection: %v", err)
	}
	if _, err := svc.GetConnection(ctx, "disabled"); !errors.Is(err, store.ErrConnectionNotFound) {
		t.Fatalf("expected deleted connection to be missing, got %v", err)
	}
}

func TestRelayStateSigningAndValidationErrors(t *testing.T) {
	noSecret := New(store.New(), nil)
	_, err := noSecret.signRelayState(callbackState{Connection: "acme"})
	if !errors.Is(err, ErrMissingStateSecret) {
		t.Fatalf("expected missing state secret from sign, got %v", err)
	}
	if _, err := noSecret.verifyRelayState("payload.signature"); !errors.Is(err, ErrMissingStateSecret) {
		t.Fatalf("expected missing state secret from verify, got %v", err)
	}

	svc := New(store.New(), []byte("01234567890123456789012345678901"))
	state := callbackState{
		Connection: "acme",
		RedirectTo: "/after",
		RequestID:  "_req123",
		IssuedAt:   time.Now().UTC().Unix(),
	}
	signed, err := svc.signRelayState(state)
	if err != nil {
		t.Fatalf("sign relay state: %v", err)
	}
	verified, err := svc.verifyRelayState(signed)
	if err != nil {
		t.Fatalf("verify relay state: %v", err)
	}
	if verified.Connection != state.Connection || verified.RequestID != state.RequestID {
		t.Fatalf("unexpected verified state: %+v", verified)
	}

	if _, err := svc.verifyRelayState("invalid"); !errors.Is(err, ErrInvalidRelayState) {
		t.Fatalf("expected invalid relay state error, got %v", err)
	}
	if _, err := svc.verifyRelayState("%%%." + strings.SplitN(signed, ".", 2)[1]); !errors.Is(err, ErrInvalidRelayState) {
		t.Fatalf("expected invalid payload encoding error, got %v", err)
	}
}

func TestBuildRedirectURLIncludesOnlyValidExtraContext(t *testing.T) {
	rawURL, err := buildRedirectURL(store.Connection{
		Slug:   "acme",
		SSOURL: "https://idp.example.com/sso",
		ExtraAuthnContext: map[string]string{
			"prompt": "login",
			" ":      "x",
			"empty":  " ",
		},
	}, "relay", "_req123")
	if err != nil {
		t.Fatalf("build redirect URL: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("prompt") != "login" {
		t.Fatalf("expected prompt param, got %q", query.Get("prompt"))
	}
	if query.Get("empty") != "" {
		t.Fatalf("expected empty key to be skipped, got %q", query.Get("empty"))
	}
	if query.Get("SAMLRequest") == "" || query.Get("RelayState") != "relay" {
		t.Fatalf("expected SAMLRequest and RelayState in query, got %v", query)
	}
}

func TestNormalizeExtraAndStringHelpers(t *testing.T) {
	normalized := normalizeExtra(map[string]string{
		" one ": " value ",
		"two":   " ",
		" ":     "three",
	})
	if len(normalized) != 1 || normalized["one"] != "value" {
		t.Fatalf("unexpected normalized extra map: %+v", normalized)
	}
	if got := firstNonEmpty(" ", "", "value"); got != "value" {
		t.Fatalf("unexpected first non-empty value: %q", got)
	}
	if got := firstNonEmpty(" ", ""); got != "" {
		t.Fatalf("expected empty first non-empty result, got %q", got)
	}
	if got := stringValue(fmtStringer("formatted")); got != "formatted" {
		t.Fatalf("unexpected stringer value: %q", got)
	}
	if got := stringValue(123); got != "" {
		t.Fatalf("expected unsupported value to stringify to empty string, got %q", got)
	}
}

func TestVerifyCertificateChainBranches(t *testing.T) {
	if err := verifyCertificateChain(nil, time.Now().UTC()); !errors.Is(err, ErrInvalidSAMLResponse) {
		t.Fatalf("expected invalid response error for empty chain, got %v", err)
	}

	_, certPEM := buildSignedResponse(t, "urn:test:idp", "_req123", "sub", "user@example.com", "User", time.Now().UTC().Add(5*time.Minute))
	certs, err := parseCertificates(certPEM)
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected one generated certificate, got %d", len(certs))
	}

	if err := verifyCertificateChain(certs, certs[0].NotBefore.Add(-3*time.Minute)); !errors.Is(err, ErrInvalidSAMLResponse) {
		t.Fatalf("expected invalid response error before certificate validity, got %v", err)
	}
	if err := verifyCertificateChain(certs, certs[0].NotAfter.Add(3*time.Minute)); !errors.Is(err, ErrInvalidSAMLResponse) {
		t.Fatalf("expected invalid response error after certificate validity, got %v", err)
	}
	validAt := certs[0].NotBefore.Add(3 * time.Minute)
	if validAt.After(certs[0].NotAfter.Add(-3 * time.Minute)) {
		validAt = certs[0].NotBefore.Add(time.Minute)
	}
	if err := verifyCertificateChain(certs, validAt); err != nil {
		t.Fatalf("expected single pinned certificate to validate, got %v", err)
	}
}

func TestParseAbsoluteURLAndExpectedAudience(t *testing.T) {
	if _, ok := parseAbsoluteURL("  "); ok {
		t.Fatal("expected blank URL to be unset")
	}
	if _, ok := parseAbsoluteURL("mailto:test@example.com"); ok {
		t.Fatal("expected non-http URL to be rejected")
	}
	if _, ok := parseAbsoluteURL("/relative/path"); ok {
		t.Fatal("expected relative URL to be rejected")
	}
	parsed, ok := parseAbsoluteURL(" https://sp.example.com/callback ")
	if !ok || parsed != "https://sp.example.com/callback" {
		t.Fatalf("unexpected parsed absolute URL: %q (ok=%v)", parsed, ok)
	}
	if got := expectedAudience(nil); got != "" {
		t.Fatalf("expected empty audience for nil connection, got %q", got)
	}
	if got := expectedAudience(&store.Connection{}); got != "" {
		t.Fatalf("expected empty audience for empty slug, got %q", got)
	}
	if got := expectedAudience(&store.Connection{Slug: "acme"}); got != "urn:aegion:sp:acme" {
		t.Fatalf("unexpected expected audience: %q", got)
	}
}

type fmtStringer string

func (f fmtStringer) String() string {
	return string(f)
}
