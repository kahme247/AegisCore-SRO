package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionTicketInstallerUsesMutualTLSAndAcknowledgesInstall(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	store := NewAdmissionTicketStore()
	server := httptest.NewUnstartedServer(NewAdmissionTicketInstallHandler(store))
	server.TLS = material.serverTLS
	server.StartTLS()
	defer server.Close()

	installer, err := NewHTTPSAdmissionTicketInstaller(server.URL, material.clientTLS, time.Second)
	if err != nil {
		t.Fatalf("NewHTTPSAdmissionTicketInstaller() error = %v", err)
	}
	ticket := validTransportTicket(0x10203040)
	if err := installer.Install(context.Background(), ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := store.Redeem(AdmissionCredentials{
		QueueID:      ticket.QueueID,
		Username:     ticket.Username,
		ContentID:    ticket.ContentID,
		HasContentID: true,
		SourceIP:     ticket.SourceIP,
	}); err != nil {
		t.Fatalf("Redeem(installed ticket) error = %v", err)
	}

	if err := performTLSInstallRequest(server.URL, material.missingClientTLS); err == nil {
		t.Fatal("mTLS listener accepted a client without a certificate")
	}
	if err := performTLSInstallRequest(server.URL, material.badClientTLS); err == nil {
		t.Fatal("mTLS listener accepted a client certificate from an untrusted CA")
	}
	if err := performTLSInstallRequest(server.URL, material.untrustedServerTLS); err == nil {
		t.Fatal("Gateway client accepted an Agent certificate from an untrusted CA")
	}
}

func TestAdmissionReadinessUsesPrivateMutualTLSListener(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	server := httptest.NewUnstartedServer(NewAdmissionTicketInstallHandler(NewAdmissionTicketStore()))
	server.TLS = material.serverTLS
	server.StartTLS()
	defer server.Close()

	installer, err := NewHTTPSAdmissionTicketInstaller(server.URL, material.clientTLS, time.Second)
	if err != nil {
		t.Fatalf("NewHTTPSAdmissionTicketInstaller() error = %v", err)
	}
	if err := installer.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+AdmissionReadinessPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: material.clientTLS.Clone()}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST readiness status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestAdmissionHandlerRejectsReadinessAndInstallsWhenRuntimeIsNotReady(t *testing.T) {
	store := NewAdmissionTicketStore()
	var ready atomic.Bool
	handler := NewAdmissionTicketInstallHandlerWithReadiness(store, ready.Load)
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + AdmissionReadinessPath)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}

	payload, err := json.Marshal(newAdmissionTicketInstallPayload(validTransportTicket(0x10203042)))
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.Post(server.URL+AdmissionTicketInstallPath, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || store.Len() != 0 {
		t.Fatalf("not-ready install status=%d tickets=%d", response.StatusCode, store.Len())
	}

	ready.Store(true)
	response, err = http.Post(server.URL+AdmissionTicketInstallPath, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || store.Len() != 1 {
		t.Fatalf("ready install status=%d tickets=%d", response.StatusCode, store.Len())
	}
}

func TestAdmissionReadinessRejectsInvalidAcknowledgements(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "not ready", body: `{"ready":false}`},
		{name: "unknown field", body: `{"ready":true,"status":"ok"}`},
		{name: "trailing value", body: `{"ready":true}{}`},
		{name: "oversized whitespace", body: `{"ready":true}` + strings.Repeat(" ", maxAdmissionTicketInstallBody)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(test.body))
			}))
			server.TLS = material.serverTLS
			server.StartTLS()
			defer server.Close()

			installer, err := NewHTTPSAdmissionTicketInstaller(server.URL, material.clientTLS, time.Second)
			if err != nil {
				t.Fatalf("NewHTTPSAdmissionTicketInstaller() error = %v", err)
			}
			if err := installer.Ready(context.Background()); err == nil {
				t.Fatal("Ready() accepted invalid acknowledgement")
			}
		})
	}
}

func TestAdmissionTicketInstallerRejectsAgentFailureAndUnboundPayload(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	store := NewAdmissionTicketStore()
	server := httptest.NewUnstartedServer(NewAdmissionTicketInstallHandler(store))
	server.TLS = material.serverTLS
	server.StartTLS()
	defer server.Close()

	installer, err := NewHTTPSAdmissionTicketInstaller(server.URL, material.clientTLS, time.Second)
	if err != nil {
		t.Fatalf("NewHTTPSAdmissionTicketInstaller() error = %v", err)
	}
	invalidTicket := validTransportTicket(0x10203041)
	invalidTicket.AccountID = 0
	if err := installer.Install(context.Background(), invalidTicket); !errors.Is(err, ErrAdmissionTicketInstallRejected) {
		t.Fatalf("Install(invalid ticket) error = %v, want ErrAdmissionTicketInstallRejected", err)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("invalid ticket was installed: store length = %d", got)
	}

	payload, err := json.Marshal(admissionTicketInstallPayload{
		QueueID:   99,
		AccountID: 7,
		Username:  "alice",
		ShardID:   1,
		ContentID: 56,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+AdmissionTicketInstallPath, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: material.clientTLS.Clone()}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST(unbound payload) error = %v", err)
	}
	defer response.Body.Close()
	if got, want := response.StatusCode, http.StatusBadRequest; got != want {
		t.Fatalf("POST(unbound payload) status = %d, want %d", got, want)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("unbound payload was installed: store length = %d", got)
	}
}

func TestAdmissionTicketInstallerPreservesPrivateDuplicateAccountOutcome(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	store := NewAdmissionTicketStore()
	server := httptest.NewUnstartedServer(NewAdmissionTicketInstallHandler(store))
	server.TLS = material.serverTLS
	server.StartTLS()
	defer server.Close()

	installer, err := NewHTTPSAdmissionTicketInstaller(server.URL, material.clientTLS, time.Second)
	if err != nil {
		t.Fatalf("NewHTTPSAdmissionTicketInstaller() error = %v", err)
	}
	first := validTransportTicket(0x10203041)
	if err := installer.Install(context.Background(), first); err != nil {
		t.Fatalf("Install(first) error = %v", err)
	}
	second := first
	second.QueueID++
	if err := installer.Install(context.Background(), second); !errors.Is(err, ErrAdmissionTicketAlreadyConnected) {
		t.Fatalf("Install(duplicate account) error = %v, want ErrAdmissionTicketAlreadyConnected", err)
	}
}

func TestAdmissionTicketTLSConfigRequiresPathsAndClientServerName(t *testing.T) {
	if _, err := LoadAdmissionTicketServerTLSConfig(MutualTLSFiles{}); err == nil {
		t.Fatal("LoadAdmissionTicketServerTLSConfig() accepted missing TLS paths")
	}
	if _, err := LoadAdmissionTicketClientTLSConfig(MutualTLSFiles{CertFile: "cert", KeyFile: "key", CAFile: "ca"}); err == nil {
		t.Fatal("LoadAdmissionTicketClientTLSConfig() accepted a missing server name")
	}

	material := newAdmissionTLSMaterial(t)
	directory := t.TempDir()
	serverCert := writeTLSFile(t, directory, "agent-cert.pem", material.serverCertPEM)
	serverKey := writeTLSFile(t, directory, "agent-key.pem", material.serverKeyPEM)
	clientCert := writeTLSFile(t, directory, "gateway-cert.pem", material.clientCertPEM)
	clientKey := writeTLSFile(t, directory, "gateway-key.pem", material.clientKeyPEM)
	caFile := writeTLSFile(t, directory, "ca.pem", material.caPEM)

	serverTLS, err := LoadAdmissionTicketServerTLSConfig(MutualTLSFiles{CertFile: serverCert, KeyFile: serverKey, CAFile: caFile})
	if err != nil {
		t.Fatalf("LoadAdmissionTicketServerTLSConfig() error = %v", err)
	}
	if got, want := serverTLS.ClientAuth, tls.RequireAndVerifyClientCert; got != want {
		t.Fatalf("server ClientAuth = %v, want %v", got, want)
	}
	clientTLS, err := LoadAdmissionTicketClientTLSConfig(MutualTLSFiles{CertFile: clientCert, KeyFile: clientKey, CAFile: caFile, ServerName: "agent.internal"})
	if err != nil {
		t.Fatalf("LoadAdmissionTicketClientTLSConfig() error = %v", err)
	}
	if got, want := clientTLS.ServerName, "agent.internal"; got != want {
		t.Fatalf("client ServerName = %q, want %q", got, want)
	}
}

func TestAdmissionTicketTLSConfigRedactsMaterialPaths(t *testing.T) {
	directory := t.TempDir()
	certFile := filepath.Join(directory, "gateway-private-cert.pem")
	keyFile := filepath.Join(directory, "gateway-private-key.pem")
	caFile := filepath.Join(directory, "agent-private-ca.pem")

	_, err := LoadAdmissionTicketClientTLSConfig(MutualTLSFiles{
		CertFile:   certFile,
		KeyFile:    keyFile,
		CAFile:     caFile,
		ServerName: "agent.internal",
	})
	if !errors.Is(err, ErrAdmissionTicketTLSMaterial) {
		t.Fatalf("LoadAdmissionTicketClientTLSConfig error = %v, want ErrAdmissionTicketTLSMaterial", err)
	}
	for _, path := range []string{certFile, keyFile, caFile} {
		if strings.Contains(err.Error(), path) {
			t.Fatalf("TLS configuration error leaked material path %q: %q", path, err)
		}
	}
}

func TestAdmissionTicketInstallServerBindsBeforeReportingStarted(t *testing.T) {
	material := newAdmissionTLSMaterial(t)
	service, err := NewAdmissionTicketInstallServer("127.0.0.1:0", material.serverTLS, NewAdmissionTicketStore())
	if err != nil {
		t.Fatalf("NewAdmissionTicketInstallServer() error = %v", err)
	}
	if err := service.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Close(context.Background())
	if service.Addr() == nil {
		t.Fatal("Start() returned before the mTLS listener was bound")
	}
	if err := service.Start(); !errors.Is(err, ErrAdmissionTicketServerStarted) {
		t.Fatalf("second Start() error = %v, want ErrAdmissionTicketServerStarted", err)
	}
}

func validTransportTicket(queueID uint32) AdmissionTicket {
	return AdmissionTicket{
		QueueID:      queueID,
		AccountID:    77,
		Username:     "Alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     netip.MustParseAddr("203.0.113.7"),
	}
}

func performTLSInstallRequest(endpoint string, config *tls.Config) error {
	transport := &http.Transport{TLSClientConfig: config.Clone()}
	client := &http.Client{Timeout: time.Second, Transport: transport}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequest(http.MethodPost, endpoint+AdmissionTicketInstallPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	return err
}

type admissionTLSMaterial struct {
	caPEM              []byte
	serverCertPEM      []byte
	serverKeyPEM       []byte
	clientCertPEM      []byte
	clientKeyPEM       []byte
	serverTLS          *tls.Config
	clientTLS          *tls.Config
	missingClientTLS   *tls.Config
	badClientTLS       *tls.Config
	untrustedServerTLS *tls.Config
}

func newAdmissionTLSMaterial(t *testing.T) admissionTLSMaterial {
	t.Helper()
	caCertificate, caKey, caPEM := newTestCA(t, "admission test CA")
	serverCert, serverCertPEM, serverKeyPEM := newTestLeafCertificate(t, caCertificate, caKey, caPEM, "agent.internal", []string{"agent.internal"}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientCert, clientCertPEM, clientKeyPEM := newTestLeafCertificate(t, caCertificate, caKey, caPEM, "gateway.internal", nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to create trusted CA pool")
	}
	otherCertificate, otherKey, otherCAPEM := newTestCA(t, "untrusted test CA")
	badClientCert, _, _ := newTestLeafCertificate(t, otherCertificate, otherKey, otherCAPEM, "bad-gateway", nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	otherRoots := x509.NewCertPool()
	if !otherRoots.AppendCertsFromPEM(otherCAPEM) {
		t.Fatal("failed to create untrusted CA pool")
	}

	return admissionTLSMaterial{
		caPEM:         caPEM,
		serverCertPEM: serverCertPEM,
		serverKeyPEM:  serverKeyPEM,
		clientCertPEM: clientCertPEM,
		clientKeyPEM:  clientKeyPEM,
		serverTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    roots,
		},
		clientTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      roots,
			ServerName:   "agent.internal",
		},
		missingClientTLS: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: "agent.internal",
		},
		badClientTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{badClientCert},
			RootCAs:      roots,
			ServerName:   "agent.internal",
		},
		untrustedServerTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      otherRoots,
			ServerName:   "agent.internal",
		},
	}
}

func newTestCA(t *testing.T, commonName string) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(CA) error = %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate(CA) error = %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate(CA) error = %v", err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newTestLeafCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, caPEM []byte, commonName string, dnsNames []string, usages []x509.ExtKeyUsage) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(leaf) error = %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		DNSNames:     dnsNames,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate(leaf) error = %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(append(certificatePEM, caPEM...), keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}
	return certificate, certificatePEM, keyPEM
}

func writeTLSFile(t *testing.T, directory, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	return path
}
