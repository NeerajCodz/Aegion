package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aegion/aegion/core/moduleauth"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/platform/config"
	corepb "github.com/aegion/aegion/internal/proto/core"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

func TestRegistryGRPCControlPlaneUsesMTLSAndModuleIdentity(t *testing.T) {
	certs := writeRegistryTestCertificates(t)
	bootstrapCredential, credentialHash, err := moduleauth.NewCredential()
	require.NoError(t, err)
	manager, err := moduleauth.NewManager(registryTestCredentialStore{credential: moduleauth.Credential{
		ID:          "credential-analytics-1",
		ModuleID:    "analytics",
		SecretHash:  credentialHash,
		Permissions: []string{"registry:register", "registry:heartbeat", "registry:deregister"},
		Audiences:   []string{"core.registry"},
		Enabled:     true,
	}}, []byte("01234567890123456789012345678901"), time.Minute)
	require.NoError(t, err)

	server := &Server{
		cfg: &config.Config{Server: config.ServerConfig{
			TLS: config.TLSConfig{
				Enabled:      true,
				CertFile:     certs.serverCertFile,
				KeyFile:      certs.serverKeyFile,
				ClientCAFile: certs.caFile,
			},
			Registry: config.ServiceRegistryConfig{GRPCListenAddr: "127.0.0.1:0"},
		}},
		log:        xlog.Default(),
		registry:   registry.New(registry.DefaultConfig(), xlog.Default()),
		moduleAuth: manager,
	}
	require.NoError(t, server.StartGRPCControlPlane())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.shutdownGRPCControlPlane(shutdownCtx))
	})

	clientTLS := &tls.Config{
		RootCAs:      certs.caPool,
		Certificates: []tls.Certificate{certs.clientCertificate},
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}
	conn, err := grpc.NewClient(server.registryGRPC.listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tokenResponse, err := corepb.NewInternalTokenServiceClient(conn).GetCurrent(ctx, &corepb.GetCurrentRequest{
		Module: "analytics",
		BootstrapProof: &corepb.GetCurrentRequest_BootstrapSecret{
			BootstrapSecret: bootstrapCredential,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokenResponse.GetToken())
	ctx = metadata.AppendToOutgoingContext(ctx, "x-aegion-internal-token", tokenResponse.GetToken())
	response, err := corepb.NewModuleRegistryClient(conn).Register(ctx, &corepb.RegisterRequest{
		Module:  "analytics",
		Version: "v1.2.3",
		Address: "analytics.internal:9100",
	})
	require.NoError(t, err)
	require.True(t, response.GetSuccess())

	mismatched, err := corepb.NewModuleRegistryClient(conn).Register(ctx, &corepb.RegisterRequest{
		Module:  "proxy",
		Version: "v1.2.3",
		Address: "proxy.internal:9100",
	})
	require.NoError(t, err)
	require.False(t, mismatched.GetSuccess())
	require.Contains(t, mismatched.GetError(), "does not match")
}

type registryTestCredentialStore struct {
	credential moduleauth.Credential
}

func (s registryTestCredentialStore) Credential(context.Context, string) (moduleauth.Credential, error) {
	return s.credential, nil
}

type registryTestCertificates struct {
	caFile            string
	serverCertFile    string
	serverKeyFile     string
	clientCertificate tls.Certificate
	caPool            *x509.CertPool
}

func writeRegistryTestCertificates(t *testing.T) registryTestCertificates {
	t.Helper()
	directory := t.TempDir()
	caCertificate, caKey := createCertificateAuthority(t)
	serverCertificate, serverKey := createSignedCertificate(t, caCertificate, caKey, false)
	clientCertificate, clientKey := createSignedCertificate(t, caCertificate, caKey, true)

	caFile := writePEMFile(t, directory, "ca.pem", "CERTIFICATE", caCertificate.Raw)
	serverCertFile := writePEMFile(t, directory, "server.pem", "CERTIFICATE", serverCertificate.Raw)
	serverKeyFile := writePEMFile(t, directory, "server-key.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey))
	clientCertFile := writePEMFile(t, directory, "client.pem", "CERTIFICATE", clientCertificate.Raw)
	clientKeyFile := writePEMFile(t, directory, "client-key.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientKey))
	clientTLSCertificate, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	require.NoError(t, err)
	caPool := x509.NewCertPool()
	caPool.AddCert(caCertificate)

	return registryTestCertificates{
		caFile:            caFile,
		serverCertFile:    serverCertFile,
		serverKeyFile:     serverKeyFile,
		clientCertificate: clientTLSCertificate,
		caPool:            caPool,
	}
}

func createCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aegion registry test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return certificate, key
}

func createSignedCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, client bool) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "aegion registry test client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	if client {
		template.Subject.CommonName = "analytics"
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.Subject.CommonName = "localhost"
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return certificate, key
}

func writePEMFile(t *testing.T, directory, name, blockType string, contents []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: contents}), 0o600))
	return path
}
