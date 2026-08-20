package management

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

const OnlinePlayersPath = "/v1/management/players"
const PlayerDisconnectsPath = "/v1/management/player-disconnects"
const BroadcastsPath = "/v1/management/broadcasts"

var ErrServerStarted = errors.New("Agent management server already started")

type PlayerControlSource interface {
	GetActivePlayers() []*model.Player
	GetPlayerByCharacterID(int) (*model.Player, error)
	BroadcastNotice(string) (int, error)
}

type Server struct {
	address   string
	tlsConfig *tls.Config
	server    *http.Server

	mu       sync.Mutex
	listener net.Listener
}

func NewServer(address string, tlsConfig *tls.Config, players PlayerControlSource) (*Server, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("Agent management listener address is required")
	}
	if tlsConfig == nil {
		return nil, errors.New("Agent management TLS configuration is required")
	}
	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert || tlsConfig.ClientCAs == nil || len(tlsConfig.Certificates) == 0 {
		return nil, errors.New("Agent management TLS must require and verify a client certificate")
	}
	if players == nil {
		return nil, errors.New("Agent management player source is required")
	}
	return &Server{
		address: address, tlsConfig: tlsConfig.Clone(),
		server: &http.Server{
			Handler:           NewHandler(players),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    4 << 10,
		},
	}, nil
}

func NewHandler(players PlayerControlSource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+OnlinePlayersPath, func(response http.ResponseWriter, _ *http.Request) {
		if players == nil {
			http.Error(response, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		active := players.GetActivePlayers()
		snapshots := make([]model.PlayerManagementSnapshot, 0, len(active))
		for _, player := range active {
			if player != nil {
				snapshots = append(snapshots, player.ManagementSnapshot())
			}
		}
		sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].UniqueID < snapshots[j].UniqueID })
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			GeneratedAt time.Time                        `json:"generatedAt"`
			Players     []model.PlayerManagementSnapshot `json:"players"`
		}{GeneratedAt: time.Now().UTC(), Players: snapshots})
	})
	mux.HandleFunc("POST "+PlayerDisconnectsPath, func(response http.ResponseWriter, request *http.Request) {
		if players == nil {
			http.Error(response, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var command struct {
			CharacterID      int    `json:"characterId"`
			ExpectedUniqueID uint32 `json:"expectedUniqueId"`
			Reason           string `json:"reason"`
		}
		if err := decoder.Decode(&command); err != nil || command.CharacterID <= 0 || command.ExpectedUniqueID == 0 {
			writeCommandError(response, http.StatusBadRequest, "invalid_command")
			return
		}
		command.Reason = strings.TrimSpace(command.Reason)
		if len(command.Reason) < 3 || len(command.Reason) > 200 {
			writeCommandError(response, http.StatusBadRequest, "invalid_reason")
			return
		}
		player, err := players.GetPlayerByCharacterID(command.CharacterID)
		if err != nil || player == nil || player.GetSession() == nil || !player.GetSession().IsInWorld() {
			writeCommandError(response, http.StatusNotFound, "not_online")
			return
		}
		if player.GetUniqueID() != command.ExpectedUniqueID {
			writeCommandError(response, http.StatusConflict, "identity_changed")
			return
		}
		name := player.ManagementSnapshot().Name
		player.GetSession().Close()
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			Disconnected bool   `json:"disconnected"`
			CharacterID  int    `json:"characterId"`
			UniqueID     uint32 `json:"uniqueId"`
			Name         string `json:"name"`
		}{Disconnected: true, CharacterID: command.CharacterID, UniqueID: command.ExpectedUniqueID, Name: name})
	})
	mux.HandleFunc("POST "+BroadcastsPath, func(response http.ResponseWriter, request *http.Request) {
		if players == nil {
			http.Error(response, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var command struct {
			Message string `json:"message"`
		}
		if err := decoder.Decode(&command); err != nil {
			writeCommandError(response, http.StatusBadRequest, "invalid_command")
			return
		}
		command.Message = strings.TrimSpace(command.Message)
		if command.Message == "" || len([]rune(command.Message)) > 200 || strings.IndexByte(command.Message, 0) >= 0 {
			writeCommandError(response, http.StatusBadRequest, "invalid_message")
			return
		}
		recipientCount, err := players.BroadcastNotice(command.Message)
		if err != nil {
			writeCommandError(response, http.StatusBadRequest, "invalid_message")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			Broadcast      bool `json:"broadcast"`
			RecipientCount int  `json:"recipientCount"`
		}{Broadcast: true, RecipientCount: recipientCount})
	})
	return mux
}

func writeCommandError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Error string `json:"error"`
	}{Error: code})
}

func (s *Server) Start() error {
	if s == nil {
		return errors.New("Agent management server is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return ErrServerStarted
	}
	listener, err := tls.Listen("tcp", s.address, s.tlsConfig)
	if err != nil {
		return fmt.Errorf("listen for Agent management requests: %w", err)
	}
	s.listener = listener
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) Addr() net.Addr {
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
