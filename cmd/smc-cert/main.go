package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type certificateBundleOptions struct {
	AgentDirectory string
	SMCDirectory   string
	ServerName     string
	Now            time.Time
	Random         io.Reader
}

type pendingFile struct {
	Path string
	Data []byte
	Mode os.FileMode
}

func main() {
	agentDirectory := flag.String("agent-dir", ".local/smc-agent-tls", "output directory mounted into Agent")
	smcDirectory := flag.String("smc-dir", ".local/smc-tls", "output directory mounted into SMC Server")
	serverName := flag.String("server-name", "agent-server", "Agent management certificate DNS name")
	flag.Parse()
	if err := generateCertificateBundle(certificateBundleOptions{
		AgentDirectory: *agentDirectory,
		SMCDirectory:   *smcDirectory,
		ServerName:     *serverName,
		Now:            time.Now().UTC(),
		Random:         rand.Reader,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("created dedicated local SMC mTLS identities for %s\n", *serverName)
}

func generateCertificateBundle(options certificateBundleOptions) error {
	options.AgentDirectory = strings.TrimSpace(options.AgentDirectory)
	options.SMCDirectory = strings.TrimSpace(options.SMCDirectory)
	options.ServerName = strings.TrimSpace(options.ServerName)
	if options.AgentDirectory == "" || options.SMCDirectory == "" || options.ServerName == "" {
		return errors.New("agent-dir, smc-dir, and server-name are required")
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), options.Random)
	if err != nil {
		return fmt.Errorf("generate SMC CA key: %w", err)
	}
	caSerial, err := randomSerial(options.Random)
	if err != nil {
		return err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "AegisCore local SMC CA"},
		NotBefore:             options.Now.Add(-5 * time.Minute),
		NotAfter:              options.Now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(options.Random, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create SMC CA certificate: %w", err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		return fmt.Errorf("parse generated SMC CA: %w", err)
	}

	serverCertificate, serverKey, err := createLeaf(options, caCertificate, caKey, "AegisCore Agent management", []string{options.ServerName}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	if err != nil {
		return err
	}
	clientCertificate, clientKey, err := createLeaf(options, caCertificate, caKey, "AegisCore SMC Server", nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	if err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	files := []pendingFile{
		{Path: filepath.Join(options.AgentDirectory, "agent-management-cert.pem"), Data: serverCertificate, Mode: 0o644},
		{Path: filepath.Join(options.AgentDirectory, "agent-management-key.pem"), Data: serverKey, Mode: 0o600},
		{Path: filepath.Join(options.AgentDirectory, "smc-ca.pem"), Data: caPEM, Mode: 0o644},
		{Path: filepath.Join(options.SMCDirectory, "smc-cert.pem"), Data: clientCertificate, Mode: 0o644},
		{Path: filepath.Join(options.SMCDirectory, "smc-key.pem"), Data: clientKey, Mode: 0o600},
		{Path: filepath.Join(options.SMCDirectory, "agent-management-ca.pem"), Data: caPEM, Mode: 0o644},
	}
	for _, file := range files {
		if _, err := os.Stat(file.Path); err == nil {
			return fmt.Errorf("refusing to overwrite existing TLS file %q", file.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect TLS output %q: %w", file.Path, err)
		}
	}
	for _, directory := range []string{options.AgentDirectory, options.SMCDirectory} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return fmt.Errorf("create TLS output directory: %w", err)
		}
	}
	written := make([]string, 0, len(files))
	for _, file := range files {
		handle, err := os.OpenFile(file.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.Mode)
		if err == nil {
			_, err = handle.Write(file.Data)
			closeErr := handle.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			for _, path := range written {
				_ = os.Remove(path)
			}
			return fmt.Errorf("write TLS output %q: %w", file.Path, err)
		}
		written = append(written, file.Path)
	}
	return nil
}

func createLeaf(options certificateBundleOptions, ca *x509.Certificate, caKey *ecdsa.PrivateKey, commonName string, dnsNames []string, usages []x509.ExtKeyUsage) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), options.Random)
	if err != nil {
		return nil, nil, fmt.Errorf("generate %s key: %w", commonName, err)
	}
	serial, err := randomSerial(options.Random)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     dnsNames,
		NotBefore:    options.Now.Add(-5 * time.Minute),
		NotAfter:     options.Now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
	}
	certificateDER, err := x509.CreateCertificate(options.Random, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s certificate: %w", commonName, err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s private key: %w", commonName, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), nil
}

func randomSerial(random io.Reader) (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(random, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}
