package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrMaterial = errors.New("mutual TLS material could not be loaded")

type Files struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

func LoadServerConfig(files Files) (*tls.Config, error) {
	certificate, roots, err := loadMaterial(files)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    roots,
	}, nil
}

func LoadClientConfig(files Files) (*tls.Config, error) {
	if strings.TrimSpace(files.ServerName) == "" {
		return nil, errors.New("mutual TLS server name is required")
	}
	certificate, roots, err := loadMaterial(files)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
		ServerName:   files.ServerName,
	}, nil
}

func loadMaterial(files Files) (tls.Certificate, *x509.CertPool, error) {
	if strings.TrimSpace(files.CertFile) == "" || strings.TrimSpace(files.KeyFile) == "" || strings.TrimSpace(files.CAFile) == "" {
		return tls.Certificate{}, nil, errors.New("mutual TLS cert, key, and CA paths are required")
	}
	certificate, err := tls.LoadX509KeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("%w: certificate", ErrMaterial)
	}
	caPEM, err := os.ReadFile(files.CAFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("%w: CA certificate", ErrMaterial)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, errors.New("mutual TLS CA contains no certificates")
	}
	return certificate, roots, nil
}
