package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// AdmissionTicketInstallPath is served only by Agent's internal mTLS
	// listener. It is intentionally unrelated to the public SRO packet stream.
	AdmissionTicketInstallPath = "/v1/admission-tickets"
	// AdmissionReadinessPath shares the private mTLS listener so Gateway can
	// stop advertising an operating shard when Agent is unreachable.
	AdmissionReadinessPath = "/v1/readiness"

	// DefaultAdmissionTicketInstallTimeout bounds Gateway's wait for the
	// Agent installation acknowledgement. Gateway must never advertise a
	// QueueID before this acknowledgement succeeds.
	DefaultAdmissionTicketInstallTimeout = 5 * time.Second
	maxAdmissionTicketInstallBody        = 4 << 10
	admissionInstallErrorHeader          = "X-AegisCore-Admission-Error"
	admissionInstallAlreadyConnected     = "already-connected"
)

var (
	ErrAdmissionTicketInstallRejected = errors.New("admission ticket installation rejected")
	ErrAdmissionTicketServerStarted   = errors.New("admission ticket server already started")
	// ErrAdmissionTicketTLSMaterial deliberately omits certificate/key/CA paths.
	// Startup callers log these errors, and those filesystem locations can be
	// operationally sensitive.
	ErrAdmissionTicketTLSMaterial = errors.New("admission ticket TLS material could not be loaded")
)

// AdmissionTicketInstaller is the narrow Gateway dependency used before it
// sends a successful 0xA102 response. An implementation must return nil only
// after Agent has durably accepted the in-memory one-time ticket.
type AdmissionTicketInstaller interface {
	Install(context.Context, AdmissionTicket) error
}

type AdmissionReadinessProbe interface {
	Ready(context.Context) error
}

// AdmissionReadiness is evaluated for every readiness probe and ticket
// installation. Returning false makes the private endpoint fail closed.
type AdmissionReadiness func() bool

// MutualTLSFiles declares the three mandatory certificate inputs. No shared
// secret fallback exists: missing or invalid paths fail startup.
type MutualTLSFiles struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

// LoadAdmissionTicketServerTLSConfig builds an mTLS server configuration that
// requires and verifies a Gateway client certificate.
func LoadAdmissionTicketServerTLSConfig(files MutualTLSFiles) (*tls.Config, error) {
	certificate, roots, err := loadMutualTLSMaterial(files)
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

// LoadAdmissionTicketClientTLSConfig builds an mTLS client configuration that
// verifies Agent's certificate name and presents Gateway's client certificate.
func LoadAdmissionTicketClientTLSConfig(files MutualTLSFiles) (*tls.Config, error) {
	if strings.TrimSpace(files.ServerName) == "" {
		return nil, errors.New("admission ticket TLS server name is required")
	}
	certificate, roots, err := loadMutualTLSMaterial(files)
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

func loadMutualTLSMaterial(files MutualTLSFiles) (tls.Certificate, *x509.CertPool, error) {
	if strings.TrimSpace(files.CertFile) == "" || strings.TrimSpace(files.KeyFile) == "" || strings.TrimSpace(files.CAFile) == "" {
		return tls.Certificate{}, nil, errors.New("admission ticket TLS cert, key, and CA paths are required")
	}
	certificate, err := tls.LoadX509KeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("%w: certificate", ErrAdmissionTicketTLSMaterial)
	}
	caPEM, err := os.ReadFile(files.CAFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("%w: CA certificate", ErrAdmissionTicketTLSMaterial)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, errors.New("admission ticket TLS CA contains no certificates")
	}
	return certificate, roots, nil
}

// AdmissionTicketInstallServer owns Agent's private mTLS endpoint. It is not
// registered with the public SRO Server and therefore cannot accept 0xE000.
type AdmissionTicketInstallServer struct {
	address   string
	tlsConfig *tls.Config
	server    *http.Server

	mu       sync.Mutex
	listener net.Listener
}

func NewAdmissionTicketInstallServer(address string, tlsConfig *tls.Config, store *AdmissionTicketStore) (*AdmissionTicketInstallServer, error) {
	return NewAdmissionTicketInstallServerWithReadiness(address, tlsConfig, store, nil)
}

func NewAdmissionTicketInstallServerWithReadiness(address string, tlsConfig *tls.Config, store *AdmissionTicketStore, readiness AdmissionReadiness) (*AdmissionTicketInstallServer, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("admission ticket listener address is required")
	}
	if tlsConfig == nil {
		return nil, errors.New("admission ticket TLS configuration is required")
	}
	if store == nil {
		return nil, errors.New("admission ticket store is required")
	}
	return &AdmissionTicketInstallServer{
		address:   address,
		tlsConfig: tlsConfig.Clone(),
		server: &http.Server{
			Handler:           NewAdmissionTicketInstallHandlerWithReadiness(store, readiness),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    4 << 10,
		},
	}, nil
}

// Start binds the internal listener before returning. A bind or TLS setup
// failure is therefore fail-closed during Agent startup.
func (s *AdmissionTicketInstallServer) Start() error {
	if s == nil {
		return errors.New("admission ticket server is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return ErrAdmissionTicketServerStarted
	}
	listener, err := tls.Listen("tcp", s.address, s.tlsConfig)
	if err != nil {
		return fmt.Errorf("listen for admission tickets: %w", err)
	}
	s.listener = listener
	go func() {
		// Shutdown closes the listener and returns http.ErrServerClosed. There is
		// no credential or ticket value in this deliberately terse error path.
		_ = s.server.Serve(listener)
	}()
	return nil
}

func (s *AdmissionTicketInstallServer) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *AdmissionTicketInstallServer) Addr() net.Addr {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// NewAdmissionTicketInstallHandler returns the strict internal HTTP handler.
// It accepts only a compact, version-independent representation of an
// admission ticket and acknowledges only after Store.Install succeeds.
func NewAdmissionTicketInstallHandler(store *AdmissionTicketStore) http.Handler {
	return NewAdmissionTicketInstallHandlerWithReadiness(store, nil)
}

func NewAdmissionTicketInstallHandlerWithReadiness(store *AdmissionTicketStore, readiness AdmissionReadiness) http.Handler {
	isReady := func() bool { return store != nil && (readiness == nil || readiness()) }
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == AdmissionReadinessPath {
			if request.Method != http.MethodGet {
				writer.Header().Set("Allow", http.MethodGet)
				http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !isReady() {
				http.Error(writer, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(struct {
				Ready bool `json:"ready"`
			}{Ready: true})
			return
		}
		if request.URL.Path != AdmissionTicketInstallPath {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isReady() {
			http.Error(writer, "service unavailable", http.StatusServiceUnavailable)
			return
		}

		request.Body = http.MaxBytesReader(writer, request.Body, maxAdmissionTicketInstallBody)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var payload admissionTicketInstallPayload
		if err := decoder.Decode(&payload); err != nil {
			http.Error(writer, "invalid admission ticket request", http.StatusBadRequest)
			return
		}
		if err := requireSingleJSONValue(decoder); err != nil {
			http.Error(writer, "invalid admission ticket request", http.StatusBadRequest)
			return
		}
		ticket, err := payload.ticket()
		if err != nil {
			http.Error(writer, "invalid admission ticket request", http.StatusBadRequest)
			return
		}
		if err := store.Install(ticket); err != nil {
			// Do not expose collision, identity, or capacity details to Gateway's
			// caller. The single typed duplicate marker is mTLS-private and lets
			// Gateway emit the stock already-connected outcome without leaking
			// ticket material to the public SRO connection.
			if errors.Is(err, ErrAdmissionTicketAlreadyConnected) {
				writer.Header().Set(admissionInstallErrorHeader, admissionInstallAlreadyConnected)
			}
			http.Error(writer, "admission ticket rejected", http.StatusConflict)
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(admissionTicketInstallResponse{Installed: true})
	})
}

// HTTPSAdmissionTicketInstaller is Gateway's mTLS client for the private
// endpoint. It intentionally has no relationship to framework/client or the
// public SRO packet protocol.
type HTTPSAdmissionTicketInstaller struct {
	endpoint          string
	readinessEndpoint string
	client            *http.Client
}

func NewHTTPSAdmissionTicketInstaller(endpoint string, tlsConfig *tls.Config, timeout time.Duration) (*HTTPSAdmissionTicketInstaller, error) {
	if tlsConfig == nil {
		return nil, errors.New("admission ticket client TLS configuration is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse admission ticket endpoint: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("admission ticket endpoint must be an HTTPS origin without credentials")
	}
	if timeout <= 0 {
		timeout = DefaultAdmissionTicketInstallTimeout
	}
	parsed.Path = AdmissionTicketInstallPath
	parsed.RawQuery = ""
	parsed.Fragment = ""

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig.Clone()
	return &HTTPSAdmissionTicketInstaller{
		endpoint: parsed.String(),
		readinessEndpoint: func() string {
			ready := *parsed
			ready.Path = AdmissionReadinessPath
			return ready.String()
		}(),
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *HTTPSAdmissionTicketInstaller) Ready(ctx context.Context) error {
	if c == nil || c.client == nil || c.readinessEndpoint == "" {
		return ErrAdmissionTicketInstallRejected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.readinessEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create Agent readiness request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("send Agent readiness request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Agent is not ready: status %d", response.StatusCode)
	}
	var acknowledgement struct {
		Ready bool `json:"ready"`
	}
	limited := &io.LimitedReader{R: response.Body, N: maxAdmissionTicketInstallBody + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&acknowledgement); err != nil || !acknowledgement.Ready {
		return errors.New("Agent returned an invalid readiness acknowledgement")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || limited.N == 0 {
		return errors.New("Agent returned an invalid readiness acknowledgement")
	}
	return nil
}

func (c *HTTPSAdmissionTicketInstaller) Install(ctx context.Context, ticket AdmissionTicket) error {
	if c == nil || c.client == nil || c.endpoint == "" {
		return ErrAdmissionTicketInstallRejected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(newAdmissionTicketInstallPayload(ticket))
	if err != nil {
		return fmt.Errorf("encode admission ticket installation: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create admission ticket request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("send admission ticket request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusConflict && response.Header.Get(admissionInstallErrorHeader) == admissionInstallAlreadyConnected {
			return ErrAdmissionTicketAlreadyConnected
		}
		return fmt.Errorf("%w: status %d", ErrAdmissionTicketInstallRejected, response.StatusCode)
	}
	var acknowledgement admissionTicketInstallResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxAdmissionTicketInstallBody))
	if err := decoder.Decode(&acknowledgement); err != nil || !acknowledgement.Installed {
		return ErrAdmissionTicketInstallRejected
	}
	return nil
}

type admissionTicketInstallPayload struct {
	QueueID   uint32 `json:"queue_id"`
	TraceID   string `json:"trace_id,omitempty"`
	AccountID uint32 `json:"account_id"`
	Username  string `json:"username"`
	ShardID   uint16 `json:"shard_id"`
	ContentID byte   `json:"content_id"`
	SourceIP  string `json:"source_ip"`
}

type admissionTicketInstallResponse struct {
	Installed bool `json:"installed"`
}

func newAdmissionTicketInstallPayload(ticket AdmissionTicket) admissionTicketInstallPayload {
	sourceIP := ""
	if ticket.SourceIP.IsValid() {
		sourceIP = ticket.SourceIP.Unmap().String()
	}
	return admissionTicketInstallPayload{
		QueueID:   ticket.QueueID,
		TraceID:   ticket.TraceID,
		AccountID: ticket.AccountID,
		Username:  ticket.Username,
		ShardID:   ticket.ShardID,
		ContentID: ticket.ContentID,
		SourceIP:  sourceIP,
	}
}

func (payload admissionTicketInstallPayload) ticket() (AdmissionTicket, error) {
	if strings.TrimSpace(payload.SourceIP) == "" {
		return AdmissionTicket{}, errors.New("source IP is required")
	}
	parsed, err := netip.ParseAddr(payload.SourceIP)
	if err != nil {
		return AdmissionTicket{}, fmt.Errorf("parse source IP: %w", err)
	}
	sourceIP := parsed.Unmap()
	if !sourceIP.IsValid() {
		return AdmissionTicket{}, errors.New("source IP is required")
	}
	return AdmissionTicket{
		QueueID:      payload.QueueID,
		TraceID:      payload.TraceID,
		AccountID:    payload.AccountID,
		Username:     payload.Username,
		ShardID:      payload.ShardID,
		ContentID:    payload.ContentID,
		HasContentID: true,
		SourceIP:     sourceIP,
	}, nil
}

func requireSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
