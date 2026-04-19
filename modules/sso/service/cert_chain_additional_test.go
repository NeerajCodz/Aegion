package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
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
	now := time.Now().UTC()
	if err := verifyCertificateChain(nil, now); err == nil {
		t.Fatal("verifyCertificateChain(nil) expected error")
	}

	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(root) error = %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Root CA"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	root := issueTestCert(t, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(leaf) error = %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Leaf"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	leaf := issueTestCert(t, leafTemplate, root, &leafKey.PublicKey, rootKey)

	if err := verifyCertificateChain([]*x509.Certificate{leaf}, now); err != nil {
		t.Fatalf("verifyCertificateChain(single cert) = %v", err)
	}

	if err := verifyCertificateChain([]*x509.Certificate{leaf, root}, now); err != nil {
		t.Fatalf("verifyCertificateChain(valid chain) = %v", err)
	}

	expiredLeaf := *leaf
	expiredLeaf.NotAfter = now.Add(-time.Hour)
	if err := verifyCertificateChain([]*x509.Certificate{&expiredLeaf}, now); err == nil {
		t.Fatal("verifyCertificateChain(expired cert) expected error")
	}

	badRootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey(bad root) error = %v", err)
	}
	badRootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Bad Root"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	badRoot := issueTestCert(t, badRootTemplate, badRootTemplate, &badRootKey.PublicKey, badRootKey)
	if err := verifyCertificateChain([]*x509.Certificate{leaf, badRoot}, now); err == nil {
		t.Fatal("verifyCertificateChain(untrusted chain) expected error")
	}
}

