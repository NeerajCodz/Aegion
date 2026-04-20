package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/sso/store"
)

func TestServiceHookedUncoveredBranches(t *testing.T) {
	origBuildAuthn := buildAuthnRequestHook
	origXMLMarshal := xmlMarshalHook
	origJSONMarshal := jsonMarshalHook
	origSignEnvelope := signEnvelopeHook
	origSystemPool := systemCertPoolHook
	t.Cleanup(func() {
		buildAuthnRequestHook = origBuildAuthn
		xmlMarshalHook = origXMLMarshal
		jsonMarshalHook = origJSONMarshal
		signEnvelopeHook = origSignEnvelope
		systemCertPoolHook = origSystemPool
	})

	t.Run("upsert connection propagates normalize errors", func(t *testing.T) {
		svc := New(store.New(), []byte("01234567890123456789012345678901"))
		if _, err := svc.UpsertConnection(context.Background(), ConnectionUpsertRequest{
			Slug: "acme",
		}); !errors.Is(err, store.ErrConnectionNotFound) {
			t.Fatalf("expected connection not found validation error, got %v", err)
		}
	})

	t.Run("build redirect handles authn request builder failure", func(t *testing.T) {
		buildAuthnRequestHook = func(connection store.Connection, requestID string) (string, error) {
			return "", errors.New("build failed")
		}
		_, err := buildRedirectURL(store.Connection{
			Slug:   "acme",
			SSOURL: "https://idp.example.com/sso",
		}, "relay", "_req")
		if err == nil {
			t.Fatal("expected buildRedirectURL to fail when authn request build fails")
		}
	})

	t.Run("build authn request handles marshal errors", func(t *testing.T) {
		xmlMarshalHook = func(v interface{}) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}
		_, err := buildAuthnRequest(store.Connection{
			Slug:   "acme",
			SSOURL: "https://idp.example.com/sso",
		}, "_req")
		if err == nil {
			t.Fatal("expected buildAuthnRequest to fail when xml marshal fails")
		}
		xmlMarshalHook = origXMLMarshal
	})

	t.Run("relay state handles marshal and signature failures", func(t *testing.T) {
		svc := New(store.New(), []byte("01234567890123456789012345678901"))
		jsonMarshalHook = func(v interface{}) ([]byte, error) {
			return nil, errors.New("json marshal failed")
		}
		if _, err := svc.signRelayState(callbackState{Connection: "acme"}); err == nil {
			t.Fatal("expected signRelayState to fail when json marshal fails")
		}

		jsonMarshalHook = origJSONMarshal
		signEnvelopeHook = func(kind string, secret, payload []byte, now time.Time) (string, error) {
			return "", errors.New("sign failed")
		}
		if _, err := svc.signRelayState(callbackState{Connection: "acme"}); err == nil {
			t.Fatal("expected signRelayState to fail when envelope signing fails")
		}
		signEnvelopeHook = origSignEnvelope
	})

	t.Run("parse response fails on expired subject confirmation", func(t *testing.T) {
		now := time.Now().UTC()
		conditionNotOnOrAfter := now.Add(5 * time.Minute).Format(time.RFC3339)
		subjectNotOnOrAfter := now.Add(-10 * time.Minute).Format(time.RFC3339)
		xmlResponse := `<Response InResponseTo="_req"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Subject><NameID>sub-1</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req" NotOnOrAfter="` + subjectNotOnOrAfter + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + conditionNotOnOrAfter + `"/></Assertion></Response>`
		_, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(xmlResponse)), nil, "_req", now)
		if !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("expected invalid SAML response for expired subject confirmation, got %v", err)
		}
	})

	t.Run("signature verification surfaces certificate parsing and validity errors", func(t *testing.T) {
		now := time.Now().UTC()
		connWithInvalidCert := &store.Connection{
			CertificatePEM: "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----",
		}
		if err := verifySAMLSignature([]byte("<Response/>"), connWithInvalidCert, now); err == nil {
			t.Fatal("expected verifySAMLSignature to fail with parseable-but-invalid cert bytes")
		}

		_, certPEM := buildSignedResponse(t, "urn:test:idp", "_req-cert", "sub", "user@example.com", "User", now.Add(5*time.Minute))
		certs, err := parseCertificates(certPEM)
		if err != nil {
			t.Fatalf("parseCertificates(valid cert) error = %v", err)
		}
		if len(certs) == 0 {
			t.Fatal("expected parsed certificate")
		}
		connWithExpiredTime := &store.Connection{CertificatePEM: certPEM}
		if err := verifySAMLSignature([]byte("<Response/>"), connWithExpiredTime, certs[0].NotAfter.Add(5*time.Minute)); err == nil {
			t.Fatal("expected verifySAMLSignature to fail with out-of-window cert time")
		}
	})

	t.Run("certificate helpers cover parse and chain branches", func(t *testing.T) {
		if _, err := parseCertificates("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"); err == nil {
			t.Fatal("expected parseCertificates to fail on invalid DER")
		}

		now := time.Now().UTC()
		rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("GenerateKey(root) error = %v", err)
		}
		rootTemplate := &x509.Certificate{
			SerialNumber:          big.NewInt(1001),
			Subject:               pkix.Name{CommonName: "Root CA"},
			NotBefore:             now.Add(-time.Hour),
			NotAfter:              now.Add(24 * time.Hour),
			IsCA:                  true,
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
			BasicConstraintsValid: true,
		}
		root := issueTestCert(t, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)

		leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("GenerateKey(leaf) error = %v", err)
		}
		leafTemplate := &x509.Certificate{
			SerialNumber:          big.NewInt(1002),
			Subject:               pkix.Name{CommonName: "Leaf"},
			NotBefore:             now.Add(-time.Hour),
			NotAfter:              now.Add(24 * time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
		}
		leaf := issueTestCert(t, leafTemplate, root, &leafKey.PublicKey, rootKey)

		midKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("GenerateKey(intermediate) error = %v", err)
		}
		midTemplate := &x509.Certificate{
			SerialNumber:          big.NewInt(1003),
			Subject:               pkix.Name{CommonName: "Intermediate Leaf"},
			NotBefore:             now.Add(-time.Hour),
			NotAfter:              now.Add(24 * time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
		}
		intermediateNonCA := issueTestCert(t, midTemplate, root, &midKey.PublicKey, rootKey)

		systemCertPoolHook = func() (*x509.CertPool, error) {
			return nil, nil
		}
		if err := verifyCertificateChain([]*x509.Certificate{leaf, intermediateNonCA, root}, now); err != nil {
			t.Fatalf("verifyCertificateChain(with nil system pool and non-CA intermediate) error = %v", err)
		}
		systemCertPoolHook = origSystemPool
	})

	t.Run("findElementByLocalName handles nil roots", func(t *testing.T) {
		if got := findElementByLocalName(nil, "Assertion"); got != nil {
			t.Fatalf("expected nil when root is nil, got %+v", got)
		}
	})
}
