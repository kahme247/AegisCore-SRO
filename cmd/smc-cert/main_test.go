package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/security/mtls"
)

func TestGenerateCertificateBundleCreatesCompatibleDedicatedIdentities(t *testing.T) {
	root := t.TempDir()
	agentDirectory := filepath.Join(root, "agent")
	smcDirectory := filepath.Join(root, "smc")
	if err := generateCertificateBundle(certificateBundleOptions{
		AgentDirectory: agentDirectory,
		SMCDirectory:   smcDirectory,
		ServerName:     "agent-server",
		Now:            time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	serverTLS, err := mtls.LoadServerConfig(mtls.Files{
		CertFile: filepath.Join(agentDirectory, "agent-management-cert.pem"),
		KeyFile:  filepath.Join(agentDirectory, "agent-management-key.pem"),
		CAFile:   filepath.Join(agentDirectory, "smc-ca.pem"),
	})
	if err != nil {
		t.Fatalf("load Agent server TLS: %v", err)
	}
	clientTLS, err := mtls.LoadClientConfig(mtls.Files{
		CertFile:   filepath.Join(smcDirectory, "smc-cert.pem"),
		KeyFile:    filepath.Join(smcDirectory, "smc-key.pem"),
		CAFile:     filepath.Join(smcDirectory, "agent-management-ca.pem"),
		ServerName: "agent-server",
	})
	if err != nil {
		t.Fatalf("load SMC client TLS: %v", err)
	}
	if serverTLS.ClientCAs == nil || clientTLS.RootCAs == nil {
		t.Fatal("generated identities did not configure both trust directions")
	}
	certificatePEM, err := os.ReadFile(filepath.Join(agentDirectory, "agent-management-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("generated Agent certificate is not PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := certificate.VerifyHostname("agent-server"); err != nil {
		t.Fatalf("Agent certificate DNS SAN: %v", err)
	}
	if err := generateCertificateBundle(certificateBundleOptions{AgentDirectory: agentDirectory, SMCDirectory: smcDirectory, ServerName: "agent-server"}); err == nil {
		t.Fatal("generator overwrote an existing identity")
	}
}
