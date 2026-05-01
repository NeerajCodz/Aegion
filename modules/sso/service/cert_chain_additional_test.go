package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/sso/store"
)

func issueTestCert(t *testing.T, tmpl, parent *x509.Certificate, pub *rsa.PublicKey, parentKey *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	return cert
}

func TestVerifyCertificateChainAdditionalBranches(t *testing.T) {
	if _, err := verifySAMLSignature(nil, nil); err == nil {
		t.Fatal("verifySAMLSignature(nil connection) expected error")
	}

	if _, err := verifySAMLSignature([]byte("<Response/>"), &store.Connection{CertificatePEM: ""}); !errors.Is(err, ErrInvalidSAMLResponse) {
		t.Fatalf("verifySAMLSignature(empty cert pem) = %v", err)
	}

	now := time.Now().UTC()
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(root) error = %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Root CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	root := issueTestCert(t, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	certs, err := parseCertificatesPEM(string(rootPEM))
	if err != nil {
		t.Fatalf("parseCertificatesPEM(valid cert) = %v", err)
	}
	if len(certs) != 1 || !certs[0].Equal(root) {
		t.Fatalf("parseCertificatesPEM() returned unexpected certificates: %#v", certs)
	}
}
