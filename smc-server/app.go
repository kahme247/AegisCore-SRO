package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	smcconfig "github.com/Harvi8/AegisCore/smc-server/config"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

//go:embed web/*
var webFiles embed.FS

type ModuleCapability struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

var moduleCapabilities = []ModuleCapability{
	{ID: "overview", Name: "Overview", Description: "Server health, shard capacity and catalog totals", Status: "available"},
	{ID: "players", Name: "Players", Description: "Persisted character directory and location inspection", Status: "available"},
	{ID: "accounts", Name: "Accounts", Description: "Account lookup, status, balances and GM roles", Status: "available"},
	{ID: "moderation", Name: "Moderation", Description: "Punishments, disconnects and IP restrictions", Status: "planned"},
	{ID: "game-control", Name: "Game Control", Description: "Audited broadcasts and safe live-world commands", Status: "available"},
	{ID: "economy", Name: "Economy", Description: "Gold, Silk and item investigations", Status: "planned"},
	{ID: "notices", Name: "Notices & Events", Description: "Gateway notices and scheduled announcements", Status: "planned"},
	{ID: "logs", Name: "Logs & Analytics", Description: "Gameplay activity and concurrency reporting", Status: "foundation"},
	{ID: "servers", Name: "Servers & Patching", Description: "Service state, shard settings and releases", Status: "foundation"},
	{ID: "administration", Name: "Administration", Description: "Operator sessions and durable command audit records", Status: "available"},
}

var auditActionPattern = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

type loginAttempt struct {
	windowStart time.Time
	count       int
}

type App struct {
	settings     smcconfig.Settings
	dashboard    DashboardSource
	directory    DirectorySource
	livePlayers  LivePlayerSource
	disconnects  PlayerDisconnectSource
	broadcasts   BroadcastSource
	audit        AuditRecorder
	sessions     *sessionStore
	passwordHash []byte
	loginMu      sync.Mutex
	loginByIP    map[string]loginAttempt
	handler      http.Handler
}

func NewApp(settings smcconfig.Settings, dashboard DashboardSource, directory DirectorySource, livePlayers LivePlayerSource, disconnects PlayerDisconnectSource, broadcasts BroadcastSource, audit AuditRecorder) (*App, error) {
	if dashboard == nil || directory == nil || audit == nil {
		return nil, fmt.Errorf("SMC dashboard, directory, and audit recorder are required")
	}
	var passwordHash []byte
	var err error
	if settings.AdminPasswordHash != "" {
		passwordHash = []byte(settings.AdminPasswordHash)
		if _, err = bcrypt.Cost(passwordHash); err != nil {
			return nil, fmt.Errorf("validate SMC_ADMIN_PASSWORD_HASH: %w", err)
		}
	} else {
		passwordHash, err = bcrypt.GenerateFromPassword([]byte(settings.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("prepare SMC administrator credential: %w", err)
		}
	}
	settings.AdminPassword = ""
	app := &App{
		settings: settings, dashboard: dashboard, directory: directory, livePlayers: livePlayers, disconnects: disconnects, broadcasts: broadcasts, audit: audit,
		sessions: newSessionStore(settings.SessionTTL), passwordHash: passwordHash,
		loginByIP: make(map[string]loginAttempt),
	}
	handler, err := app.routes()
	if err != nil {
		return nil, err
	}
	app.handler = app.securityHeaders(handler)
	return app, nil
}

func (a *App) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	a.handler.ServeHTTP(response, request)
}

func (a *App) routes() (http.Handler, error) {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, fmt.Errorf("open embedded SMC web assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "service": "smc-server"})
	})
	mux.HandleFunc("POST /api/v1/session", a.handleLogin)
	mux.Handle("GET /api/v1/session", a.authenticated(http.HandlerFunc(a.handleSession)))
	mux.Handle("DELETE /api/v1/session", a.authenticated(http.HandlerFunc(a.handleLogout)))
	mux.Handle("GET /api/v1/dashboard", a.authenticated(http.HandlerFunc(a.handleDashboard)))
	mux.Handle("GET /api/v1/accounts", a.authenticated(http.HandlerFunc(a.handleAccountSearch)))
	mux.Handle("GET /api/v1/characters", a.authenticated(http.HandlerFunc(a.handleCharacterSearch)))
	mux.Handle("GET /api/v1/online-players", a.authenticated(http.HandlerFunc(a.handleOnlinePlayers)))
	mux.Handle("POST /api/v1/player-disconnects", a.authenticated(http.HandlerFunc(a.handlePlayerDisconnect)))
	mux.Handle("POST /api/v1/broadcasts", a.authenticated(http.HandlerFunc(a.handleBroadcast)))
	mux.Handle("GET /api/v1/audit-events", a.authenticated(http.HandlerFunc(a.handleAuditEvents)))
	mux.Handle("GET /api/v1/modules", a.authenticated(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{"modules": moduleCapabilities})
	})))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /", func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, readErr := webFiles.ReadFile("web/index.html")
		if readErr != nil {
			http.Error(response, "SMC interface unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = response.Write(data)
	})
	return mux, nil
}

func (a *App) handleLogin(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin login is not allowed")
		return
	}
	remoteIP := clientIP(request.RemoteAddr)
	if !a.allowLoginAttempt(remoteIP) {
		writeAPIError(response, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid login request")
		return
	}
	usernameMatches := subtle.ConstantTimeCompare([]byte(input.Username), []byte(a.settings.AdminUsername)) == 1
	passwordErr := bcrypt.CompareHashAndPassword(a.passwordHash, []byte(input.Password))
	if !usernameMatches || passwordErr != nil {
		a.recordBestEffort(AuditEvent{Action: "operator.login", Outcome: "denied", Operator: input.Username, RemoteAddr: remoteIP})
		writeAPIError(response, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, session, err := a.sessions.create(a.settings.AdminUsername)
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "could not create operator session")
		return
	}
	if err := a.audit.Record(AuditEvent{Action: "operator.login", Outcome: "success", Operator: session.Username, RemoteAddr: remoteIP}); err != nil {
		a.sessions.delete(token)
		log.WithError(err).Error("record SMC login audit event")
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable")
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: a.settings.CookieSecure, SameSite: http.SameSiteStrictMode,
		Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()),
	})
	writeJSON(response, http.StatusOK, map[string]any{"operator": session.Username, "expiresAt": session.ExpiresAt})
}

func (a *App) handleSession(response http.ResponseWriter, request *http.Request) {
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	writeJSON(response, http.StatusOK, map[string]any{"operator": session.Username, "expiresAt": session.ExpiresAt})
}

func (a *App) handleLogout(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin logout is not allowed")
		return
	}
	cookie, _ := request.Cookie(sessionCookieName)
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	if cookie != nil {
		a.sessions.delete(cookie.Value)
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.settings.CookieSecure, SameSite: http.SameSiteStrictMode})
	a.recordBestEffort(AuditEvent{Action: "operator.logout", Outcome: "success", Operator: session.Username, RemoteAddr: clientIP(request.RemoteAddr)})
	response.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDashboard(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := contextWithTimeout(request, 3*time.Second)
	defer cancel()
	snapshot, err := a.dashboard.Snapshot(ctx)
	if err != nil {
		log.WithError(err).Error("load SMC dashboard")
		writeAPIError(response, http.StatusServiceUnavailable, "dashboard data is temporarily unavailable")
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
}

func (a *App) handleAccountSearch(response http.ResponseWriter, request *http.Request) {
	query, limit, ok := directorySearchParameters(response, request)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(request, 3*time.Second)
	defer cancel()
	items, err := a.directory.SearchAccounts(ctx, query, limit)
	if err != nil {
		log.WithError(err).Error("search SMC accounts")
		writeAPIError(response, http.StatusServiceUnavailable, "account directory is temporarily unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "query": query, "limit": limit})
}

func (a *App) handleCharacterSearch(response http.ResponseWriter, request *http.Request) {
	query, limit, ok := directorySearchParameters(response, request)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(request, 3*time.Second)
	defer cancel()
	items, err := a.directory.SearchCharacters(ctx, query, limit)
	if err != nil {
		log.WithError(err).Error("search SMC characters")
		writeAPIError(response, http.StatusServiceUnavailable, "character directory is temporarily unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "query": query, "limit": limit})
}

func (a *App) handleOnlinePlayers(response http.ResponseWriter, request *http.Request) {
	if a.livePlayers == nil {
		writeJSON(response, http.StatusOK, map[string]any{"available": false, "players": []LivePlayerRecord{}})
		return
	}
	ctx, cancel := contextWithTimeout(request, 3*time.Second)
	defer cancel()
	snapshot, err := a.livePlayers.OnlinePlayers(ctx)
	if err != nil {
		log.WithError(err).Warn("load Agent online-player snapshot")
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"available": true, "generatedAt": snapshot.GeneratedAt, "players": snapshot.Players})
}

func (a *App) handlePlayerDisconnect(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin commands are not allowed")
		return
	}
	if a.disconnects == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var command PlayerDisconnectCommand
	if err := decoder.Decode(&command); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid disconnect command")
		return
	}
	command.Reason = strings.TrimSpace(command.Reason)
	if command.CharacterID <= 0 || command.ExpectedUniqueID == 0 || len(command.Reason) < 3 || len(command.Reason) > 200 {
		writeAPIError(response, http.StatusBadRequest, "characterId, expectedUniqueId, and a 3..200 character reason are required")
		return
	}
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	event := AuditEvent{
		Action: "player.disconnect", Outcome: "attempt", Operator: session.Username,
		RemoteAddr: clientIP(request.RemoteAddr), Target: fmt.Sprintf("character:%d", command.CharacterID),
		Details: map[string]any{"expectedUniqueId": command.ExpectedUniqueID, "reason": command.Reason},
	}
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC disconnect attempt")
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable; command was not sent")
		return
	}
	// Detach terminal details from the already-recorded attempt. AuditRecorder
	// implementations may retain the event instead of serializing immediately.
	event.Details = map[string]any{"expectedUniqueId": command.ExpectedUniqueID, "reason": command.Reason}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	result, err := a.disconnects.DisconnectPlayer(ctx, command)
	if err != nil {
		event.Outcome = "failed"
		event.Details["failure"] = disconnectFailureCode(err)
		a.recordBestEffort(event)
		switch {
		case errors.Is(err, ErrAgentPlayerNotOnline):
			writeAPIError(response, http.StatusNotFound, "player is no longer online")
		case errors.Is(err, ErrAgentPlayerIdentityChanged):
			writeAPIError(response, http.StatusConflict, "player reconnected; refresh before retrying")
		default:
			log.WithError(err).Warn("execute Agent player disconnect")
			writeAPIError(response, http.StatusServiceUnavailable, "Agent disconnect command failed")
		}
		return
	}
	event.Outcome = "success"
	event.Details["name"] = result.Name
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC disconnect success")
		writeAPIError(response, http.StatusServiceUnavailable, "player disconnected but success audit could not be persisted")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func disconnectFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrAgentPlayerNotOnline):
		return "not_online"
	case errors.Is(err, ErrAgentPlayerIdentityChanged):
		return "identity_changed"
	default:
		return "agent_unavailable"
	}
}

func (a *App) handleBroadcast(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin commands are not allowed")
		return
	}
	if a.broadcasts == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var command BroadcastCommand
	if err := decoder.Decode(&command); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid broadcast command")
		return
	}
	command.Message = strings.TrimSpace(command.Message)
	if !utf8.ValidString(command.Message) || command.Message == "" || utf8.RuneCountInString(command.Message) > 200 || strings.IndexByte(command.Message, 0) >= 0 {
		writeAPIError(response, http.StatusBadRequest, "message must contain 1..200 characters")
		return
	}
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	event := AuditEvent{
		Action: "world.broadcast", Outcome: "attempt", Operator: session.Username,
		RemoteAddr: clientIP(request.RemoteAddr), Target: "world:all",
		Details: map[string]any{"message": command.Message},
	}
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC broadcast attempt")
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable; command was not sent")
		return
	}
	event.Details = map[string]any{"message": command.Message}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	result, err := a.broadcasts.Broadcast(ctx, command)
	if err != nil {
		event.Outcome = "failed"
		event.Details["failure"] = "agent_unavailable"
		a.recordBestEffort(event)
		log.WithError(err).Warn("execute Agent broadcast")
		writeAPIError(response, http.StatusServiceUnavailable, "Agent broadcast command failed")
		return
	}
	event.Outcome = "success"
	event.Details["recipientCount"] = result.RecipientCount
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC broadcast success")
		writeAPIError(response, http.StatusServiceUnavailable, "notice was broadcast but success audit could not be persisted")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a *App) handleAuditEvents(response http.ResponseWriter, request *http.Request) {
	limit := 50
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 200 {
			writeAPIError(response, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	action := strings.TrimSpace(request.URL.Query().Get("action"))
	if action != "" && !auditActionPattern.MatchString(action) {
		writeAPIError(response, http.StatusBadRequest, "action filter is invalid")
		return
	}
	ctx, cancel := contextWithTimeout(request, 3*time.Second)
	defer cancel()
	events, err := a.audit.Recent(ctx, AuditQuery{Action: action, Limit: limit})
	if err != nil {
		log.WithError(err).Warn("read SMC audit events")
		writeAPIError(response, http.StatusServiceUnavailable, "audit records are unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": events, "limit": limit, "action": action})
}

func directorySearchParameters(response http.ResponseWriter, request *http.Request) (string, int, bool) {
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	if len(query) < 1 || len(query) > 64 {
		writeAPIError(response, http.StatusBadRequest, "q must contain 1..64 characters")
		return "", 0, false
	}
	limit := 25
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 50 {
			writeAPIError(response, http.StatusBadRequest, "limit must be between 1 and 50")
			return "", 0, false
		}
		limit = parsed
	}
	return query, limit, true
}

type sessionContextKey struct{}

func (a *App) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(response, http.StatusUnauthorized, "authentication required")
			return
		}
		session, ok := a.sessions.get(cookie.Value)
		if !ok {
			writeAPIError(response, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(response, request.WithContext(withSession(request, session)))
	})
}

func (a *App) allowLoginAttempt(ip string) bool {
	const maxAttempts = 10
	const window = 15 * time.Minute
	now := time.Now()
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	attempt := a.loginByIP[ip]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) >= window {
		attempt = loginAttempt{windowStart: now}
	}
	attempt.count++
	a.loginByIP[ip] = attempt
	return attempt.count <= maxAttempts
}

func (a *App) recordBestEffort(event AuditEvent) {
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC audit event")
	}
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func sameOrigin(request *http.Request) bool {
	if strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+request.Host || origin == "https://"+request.Host
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeAPIError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func contextWithTimeout(request *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(request.Context(), timeout)
}

func withSession(request *http.Request, session operatorSession) context.Context {
	return context.WithValue(request.Context(), sessionContextKey{}, session)
}
