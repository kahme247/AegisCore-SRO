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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/scheduler"
	frameworkserver "github.com/Harvi8/AegisCore/framework/server"
)

const OnlinePlayersPath = "/v1/management/players"
const PlayerDisconnectsPath = "/v1/management/player-disconnects"
const BroadcastsPath = "/v1/management/broadcasts"
const HealthPath = "/v1/management/health"
const SchedulesPath = "/v1/management/schedules"

type HealthSource interface {
	DatabaseQueryCount() int
	MessageDispatch() frameworkserver.DispatchMetrics
}

var ErrServerStarted = errors.New("Agent management server already started")

type PlayerControlSource interface {
	GetActivePlayers() []*model.Player
	GetPlayerByCharacterID(int) (*model.Player, error)
	BroadcastNotice(string) (int, error)
}

type SchedulerSource interface {
	Create(context.Context, scheduler.Definition) (scheduler.Definition, error)
	List(context.Context, int) ([]scheduler.Definition, error)
	Get(context.Context, string) (scheduler.Definition, error)
	HasHandler(string) bool
	SetEnabled(context.Context, string, uint64, bool) (scheduler.Definition, error)
	ListRuns(context.Context, string, int) ([]scheduler.RunRecord, error)
}

type Server struct {
	address   string
	tlsConfig *tls.Config
	server    *http.Server

	mu       sync.Mutex
	listener net.Listener
}

func NewServer(address string, tlsConfig *tls.Config, players PlayerControlSource, healthSources ...HealthSource) (*Server, error) {
	return NewServerWithScheduler(address, tlsConfig, players, nil, healthSources...)
}

func NewServerWithScheduler(address string, tlsConfig *tls.Config, players PlayerControlSource, schedules SchedulerSource, healthSources ...HealthSource) (*Server, error) {
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
			Handler:           NewHandlerWithScheduler(players, schedules, healthSources...),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    4 << 10,
		},
	}, nil
}

func NewHandler(players PlayerControlSource, healthSources ...HealthSource) http.Handler {
	return NewHandlerWithScheduler(players, nil, healthSources...)
}

func NewHandlerWithScheduler(players PlayerControlSource, schedules SchedulerSource, healthSources ...HealthSource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, func(response http.ResponseWriter, request *http.Request) {
		now := time.Now()
		game := manager.GameHealth(now)
		dbQueries := 0
		dispatch := frameworkserver.DispatchMetrics{}
		competitive := service.CompetitiveStatus{}
		if len(healthSources) > 0 && healthSources[0] != nil {
			dbQueries = healthSources[0].DatabaseQueryCount()
			dispatch = healthSources[0].MessageDispatch()
			if source, ok := healthSources[0].(interface {
				CompetitiveStatus() service.CompetitiveStatus
			}); ok {
				competitive = source.CompetitiveStatus()
			}
		}
		objects := 0
		if players != nil {
			if source, ok := players.(interface{ ObjectCount() int }); ok {
				objects = source.ObjectCount()
			} else {
				objects = len(players.GetActivePlayers())
			}
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			GeneratedAt        time.Time                       `json:"generatedAt"`
			ObjectCount        int                             `json:"objectCount"`
			DatabaseQueryCount int                             `json:"databaseQueryCount"`
			GameUpdate         manager.GameHealthSnapshot      `json:"gameUpdate"`
			MessageDispatch    frameworkserver.DispatchMetrics `json:"messageDispatch"`
			Competitive        service.CompetitiveStatus       `json:"competitive"`
		}{now.UTC(), objects, dbQueries, game, dispatch, competitive})
	})
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
	mux.HandleFunc("GET "+SchedulesPath, func(response http.ResponseWriter, request *http.Request) {
		if schedules == nil {
			writeCommandError(response, http.StatusServiceUnavailable, "scheduler_unavailable")
			return
		}
		definitions, err := schedules.List(request.Context(), 500)
		if err != nil {
			writeCommandError(response, http.StatusInternalServerError, "scheduler_query_failed")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			Schedules []scheduler.Definition `json:"schedules"`
		}{Schedules: definitions})
	})
	mux.HandleFunc("GET "+SchedulesPath+"/{id}", func(response http.ResponseWriter, request *http.Request) {
		if schedules == nil {
			writeCommandError(response, http.StatusServiceUnavailable, "scheduler_unavailable")
			return
		}
		definition, err := schedules.Get(request.Context(), request.PathValue("id"))
		if errors.Is(err, scheduler.ErrDefinitionNotFound) {
			writeCommandError(response, http.StatusNotFound, "schedule_not_found")
			return
		}
		if err != nil {
			writeCommandError(response, http.StatusInternalServerError, "scheduler_query_failed")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(definition)
	})
	mux.HandleFunc("POST "+SchedulesPath, func(response http.ResponseWriter, request *http.Request) {
		if schedules == nil {
			writeCommandError(response, http.StatusServiceUnavailable, "scheduler_unavailable")
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, 16<<10)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var definition scheduler.Definition
		if err := decoder.Decode(&definition); err != nil || definition.Version != 0 || !definition.NextRunAt.IsZero() {
			writeCommandError(response, http.StatusBadRequest, "invalid_schedule")
			return
		}
		if !schedules.HasHandler(definition.Handler) {
			writeCommandError(response, http.StatusBadRequest, "unknown_schedule_handler")
			return
		}
		created, err := schedules.Create(request.Context(), definition)
		if err != nil {
			writeCommandError(response, http.StatusBadRequest, "invalid_schedule")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(response).Encode(created)
	})
	mux.HandleFunc("PATCH "+SchedulesPath+"/{id}", func(response http.ResponseWriter, request *http.Request) {
		if schedules == nil {
			writeCommandError(response, http.StatusServiceUnavailable, "scheduler_unavailable")
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var command struct {
			ExpectedVersion uint64 `json:"expectedVersion"`
			Enabled         *bool  `json:"enabled"`
		}
		if err := decoder.Decode(&command); err != nil || command.ExpectedVersion == 0 || command.Enabled == nil {
			writeCommandError(response, http.StatusBadRequest, "invalid_schedule_update")
			return
		}
		definition, err := schedules.SetEnabled(request.Context(), request.PathValue("id"), command.ExpectedVersion, *command.Enabled)
		if errors.Is(err, scheduler.ErrDefinitionChanged) {
			writeCommandError(response, http.StatusConflict, "schedule_changed")
			return
		}
		if errors.Is(err, scheduler.ErrDefinitionNotFound) {
			writeCommandError(response, http.StatusNotFound, "schedule_not_found")
			return
		}
		if err != nil {
			writeCommandError(response, http.StatusBadRequest, "invalid_schedule_update")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(definition)
	})
	mux.HandleFunc("GET "+SchedulesPath+"/{id}/runs", func(response http.ResponseWriter, request *http.Request) {
		if schedules == nil {
			writeCommandError(response, http.StatusServiceUnavailable, "scheduler_unavailable")
			return
		}
		limit := 100
		if raw := request.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 500 {
				writeCommandError(response, http.StatusBadRequest, "invalid_limit")
				return
			}
			limit = parsed
		}
		runs, err := schedules.ListRuns(request.Context(), request.PathValue("id"), limit)
		if err != nil {
			writeCommandError(response, http.StatusInternalServerError, "scheduler_query_failed")
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			Runs []scheduler.RunRecord `json:"runs"`
		}{Runs: runs})
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
