package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/sso/store"
)

func TestServiceHookedUncoveredBranches(t *testing.T) {
	t.Run("upsert connection propagates normalize errors", func(t *testing.T) {
		svc := New(store.New(), []byte("01234567890123456789012345678901"))
		if _, err := svc.UpsertConnection(context.Background(), ConnectionUpsertRequest{
			Slug: "acme",
		}); !errors.Is(err, store.ErrConnectionNotFound) {
			t.Fatalf("expected connection not found validation error, got %v", err)
		}
	})

	t.Run("build redirect rejects invalid sso url", func(t *testing.T) {
		_, err := buildRedirectURL(store.Connection{
			Slug:   "acme",
			SSOURL: "://bad",
		}, "relay", "_req")
		if err == nil {
			t.Fatal("expected buildRedirectURL to fail on invalid url")
		}
	})

	t.Run("build authn request generates an id when request id is blank", func(t *testing.T) {
		_, err := buildAuthnRequest(store.Connection{
			Slug:   "acme",
			SSOURL: "https://idp.example.com/sso",
		}, "")
		if err != nil {
			t.Fatalf("buildAuthnRequest(empty request id) = %v", err)
		}
	})

	t.Run("relay state rejects tampering", func(t *testing.T) {
		svc := New(store.New(), []byte("01234567890123456789012345678901"))
		signed, err := svc.signRelayState(callbackState{Connection: "acme", RequestID: "_req"})
		if err != nil {
			t.Fatalf("signRelayState() error = %v", err)
		}
		tampered := signed[:len(signed)-1] + "x"
		if _, err := svc.verifyRelayState(tampered); !errors.Is(err, ErrInvalidRelayState) {
			t.Fatalf("verifyRelayState(tampered) = %v", err)
		}
	})

	t.Run("parse response fails on expired subject confirmation", func(t *testing.T) {
		now := time.Now().UTC()
		conditionNotOnOrAfter := now.Add(5 * time.Minute).Format(time.RFC3339)
		subjectNotOnOrAfter := now.Add(-10 * time.Minute).Format(time.RFC3339)
		xmlResponse := `<Response InResponseTo="_req"><Status><StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></Status><Assertion><Subject><NameID>sub-1</NameID><SubjectConfirmation><SubjectConfirmationData InResponseTo="_req" NotOnOrAfter="` + subjectNotOnOrAfter + `"/></SubjectConfirmation></Subject><Conditions NotOnOrAfter="` + conditionNotOnOrAfter + `"/></Assertion></Response>`
		_, err := parseSAMLResponse(base64.StdEncoding.EncodeToString([]byte(xmlResponse)), nil, "_req", nil, now)
		if !errors.Is(err, ErrInvalidSAMLResponse) {
			t.Fatalf("expected invalid SAML response for expired subject confirmation, got %v", err)
		}
	})

	t.Run("signature verification surfaces certificate parsing errors", func(t *testing.T) {
		connWithInvalidCert := &store.Connection{
			CertificatePEM: "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----",
		}
		if _, _, err := verifySAMLSignature([]byte("<Response/>"), connWithInvalidCert); err == nil {
			t.Fatal("expected verifySAMLSignature to fail with parseable-but-invalid cert bytes")
		}

		signer := newSAMLTestSigner(t)
		certs, err := parseCertificatesPEM(signer.certificatePEM)
		if err != nil {
			t.Fatalf("parseCertificatesPEM(valid cert) error = %v", err)
		}
		if len(certs) == 0 {
			t.Fatal("expected parsed certificate")
		}
	})

	t.Run("certificate helpers reject invalid der", func(t *testing.T) {
		if _, err := parseCertificatesPEM("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"); err == nil {
			t.Fatal("expected parseCertificatesPEM to fail on invalid DER")
		}
	})

	t.Run("findElementByLocalName handles nil roots", func(t *testing.T) {
		if got := findElementByLocalName(nil, "Assertion"); got != nil {
			t.Fatalf("expected nil when root is nil, got %+v", got)
		}
	})
}
