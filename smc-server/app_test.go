package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	smcconfig "github.com/Harvi8/AegisCore/smc-server/config"
)

type fixedDashboard struct {
	snapshot DashboardSnapshot
}

type fixedDirectory struct {
	accounts   []AccountRecord
	characters []CharacterRecord
}

func (d fixedDirectory) SearchAccounts(context.Context, string, int) ([]AccountRecord, error) {
	return d.accounts, nil
}

func (d fixedDirectory) SearchCharacters(context.Context, string, int) ([]CharacterRecord, error) {
	return d.characters, nil
}

func (d fixedDashboard) Snapshot(context.Context) (DashboardSnapshot, error) {
	return d.snapshot, nil
}

type memoryAudit struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (a *memoryAudit) Record(event AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

func (a *memoryAudit) Recent(_ context.Context, query AuditQuery) ([]AuditEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	events := make([]AuditEvent, 0, query.Limit)
	for index := len(a.events) - 1; index >= 0 && len(events) < query.Limit; index-- {
		if query.Action == "" || a.events[index].Action == query.Action {
			events = append(events, a.events[index])
		}
	}
	return events, nil
}

func newTestApp(t *testing.T) (*App, *memoryAudit) {
	t.Helper()
	audit := &memoryAudit{}
	app, err := NewApp(smcconfig.Settings{
		AdminUsername: "operator",
		AdminPassword: "correct horse battery staple",
		SessionTTL:    time.Hour,
	}, fixedDashboard{snapshot: DashboardSnapshot{
		GeneratedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		Totals:      DashboardTotals{Accounts: 4, Characters: 7},
		Services:    []ServiceHealth{{ID: "gateway", State: "reachable"}},
		Shards:      []ShardSummary{{ID: 1, Name: "AegisCore", Capacity: 1000}},
	}}, fixedDirectory{
		accounts:   []AccountRecord{{ID: 1, Username: "account"}},
		characters: []CharacterRecord{{ID: 2, Name: "Character"}},
	}, nil, nil, nil, audit)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	return app, audit
}

func authenticatedRequest(t *testing.T, app *App, method, target string) *http.Request {
	t.Helper()
	token, _, err := app.sessions.create("operator")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	return request
}

func TestDashboardRequiresAuthenticatedOperator(t *testing.T) {
	app, _ := newTestApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if got := response.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("missing Content-Security-Policy")
	}
}

func TestOperatorCanLoginAndLoadDashboard(t *testing.T) {
	app, audit := newTestApp(t)
	body, err := json.Marshal(map[string]string{"username": "operator", "password": "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewReader(body))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	app.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", loginResponse.Code, http.StatusOK, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("login cookies = %#v", cookies)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	dashboardRequest.AddCookie(cookies[0])
	dashboardResponse := httptest.NewRecorder()
	app.ServeHTTP(dashboardResponse, dashboardRequest)
	if dashboardResponse.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d", dashboardResponse.Code, http.StatusOK)
	}
	var snapshot DashboardSnapshot
	if err := json.Unmarshal(dashboardResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if snapshot.Totals.Accounts != 4 || len(snapshot.Shards) != 1 {
		t.Fatalf("dashboard = %#v", snapshot)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "operator.login" || audit.events[0].Outcome != "success" {
		t.Fatalf("audit events = %#v", audit.events)
	}
}

func TestLoginRejectsCrossOriginAndInvalidCredentials(t *testing.T) {
	app, audit := newTestApp(t)
	crossOrigin := httptest.NewRequest(http.MethodPost, "http://smc.test/api/v1/session", bytes.NewBufferString(`{"username":"operator","password":"wrong"}`))
	crossOrigin.Header.Set("Origin", "https://attacker.test")
	crossOriginResponse := httptest.NewRecorder()
	app.ServeHTTP(crossOriginResponse, crossOrigin)
	if crossOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want %d", crossOriginResponse.Code, http.StatusForbidden)
	}

	invalid := httptest.NewRequest(http.MethodPost, "http://smc.test/api/v1/session", bytes.NewBufferString(`{"username":"operator","password":"wrong"}`))
	invalidResponse := httptest.NewRecorder()
	app.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d, want %d", invalidResponse.Code, http.StatusUnauthorized)
	}
	if len(audit.events) != 1 || audit.events[0].Outcome != "denied" {
		t.Fatalf("audit events = %#v", audit.events)
	}
}

func TestSessionStoreExpiresTokens(t *testing.T) {
	store := newSessionStore(time.Hour)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	token, _, err := store.create("operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.get(token); !ok {
		t.Fatal("new session was not found")
	}
	now = now.Add(time.Hour)
	if _, ok := store.get(token); ok {
		t.Fatal("expired session was accepted")
	}
}

func TestDirectoryEndpointsRequireValidatedBoundedSearch(t *testing.T) {
	app, _ := newTestApp(t)

	accountRequest := authenticatedRequest(t, app, http.MethodGet, "/api/v1/accounts?q=acc&limit=2")
	accountResponse := httptest.NewRecorder()
	app.ServeHTTP(accountResponse, accountRequest)
	if accountResponse.Code != http.StatusOK {
		t.Fatalf("account search status = %d, want %d; body=%s", accountResponse.Code, http.StatusOK, accountResponse.Body.String())
	}
	var accountResult struct {
		Items []AccountRecord `json:"items"`
		Limit int             `json:"limit"`
	}
	if err := json.Unmarshal(accountResponse.Body.Bytes(), &accountResult); err != nil {
		t.Fatal(err)
	}
	if len(accountResult.Items) != 1 || accountResult.Items[0].Username != "account" || accountResult.Limit != 2 {
		t.Fatalf("account result = %#v", accountResult)
	}

	invalidRequest := authenticatedRequest(t, app, http.MethodGet, "/api/v1/characters?limit=500")
	invalidResponse := httptest.NewRecorder()
	app.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid search status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}

type fixedLivePlayers struct {
	snapshot LivePlayerSnapshot
}

func (f fixedLivePlayers) OnlinePlayers(context.Context) (LivePlayerSnapshot, error) {
	return f.snapshot, nil
}

func TestOnlinePlayersReportsDisabledAndConfiguredStates(t *testing.T) {
	app, _ := newTestApp(t)
	disabledRequest := authenticatedRequest(t, app, http.MethodGet, "/api/v1/online-players")
	disabledResponse := httptest.NewRecorder()
	app.ServeHTTP(disabledResponse, disabledRequest)
	if disabledResponse.Code != http.StatusOK || !bytes.Contains(disabledResponse.Body.Bytes(), []byte(`"available":false`)) {
		t.Fatalf("disabled response = %d %s", disabledResponse.Code, disabledResponse.Body.String())
	}

	app.livePlayers = fixedLivePlayers{snapshot: LivePlayerSnapshot{Players: []LivePlayerRecord{{CharacterID: 7, Name: "Alice"}}}}
	configuredRequest := authenticatedRequest(t, app, http.MethodGet, "/api/v1/online-players")
	configuredResponse := httptest.NewRecorder()
	app.ServeHTTP(configuredResponse, configuredRequest)
	if configuredResponse.Code != http.StatusOK || !bytes.Contains(configuredResponse.Body.Bytes(), []byte(`"available":true`)) || !bytes.Contains(configuredResponse.Body.Bytes(), []byte(`"Alice"`)) {
		t.Fatalf("configured response = %d %s", configuredResponse.Code, configuredResponse.Body.String())
	}
}

type fixedDisconnects struct {
	command PlayerDisconnectCommand
	result  PlayerDisconnectResult
	err     error
}

func (f *fixedDisconnects) DisconnectPlayer(_ context.Context, command PlayerDisconnectCommand) (PlayerDisconnectResult, error) {
	f.command = command
	return f.result, f.err
}

func TestPlayerDisconnectIsValidatedAndAuditedBeforeSuccess(t *testing.T) {
	app, audit := newTestApp(t)
	disconnects := &fixedDisconnects{result: PlayerDisconnectResult{Disconnected: true, CharacterID: 7, UniqueID: 100, Name: "Alice"}}
	app.disconnects = disconnects
	request := authenticatedRequest(t, app, http.MethodPost, "/api/v1/player-disconnects")
	request.Body = io.NopCloser(bytes.NewBufferString(`{"characterId":7,"expectedUniqueId":100,"reason":"Repeated abuse"}`))
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if disconnects.command.CharacterID != 7 || disconnects.command.ExpectedUniqueID != 100 || disconnects.command.Reason != "Repeated abuse" {
		t.Fatalf("command = %#v", disconnects.command)
	}
	if len(audit.events) != 2 || audit.events[0].Outcome != "attempt" || audit.events[1].Outcome != "success" || audit.events[1].Target != "character:7" {
		t.Fatalf("audit events = %#v", audit.events)
	}
}

func TestPlayerDisconnectRejectsStaleRuntimeIdentity(t *testing.T) {
	app, audit := newTestApp(t)
	app.disconnects = &fixedDisconnects{err: ErrAgentPlayerIdentityChanged}
	request := authenticatedRequest(t, app, http.MethodPost, "/api/v1/player-disconnects")
	request.Body = io.NopCloser(bytes.NewBufferString(`{"characterId":7,"expectedUniqueId":99,"reason":"Operator request"}`))
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if len(audit.events) != 2 || audit.events[1].Outcome != "failed" || audit.events[1].Details["failure"] != "identity_changed" {
		t.Fatalf("audit events = %#v", audit.events)
	}
	if _, mutated := audit.events[0].Details["failure"]; mutated {
		t.Fatalf("attempt audit was mutated by terminal outcome: %#v", audit.events[0])
	}
}

type fixedBroadcasts struct {
	command BroadcastCommand
	result  BroadcastResult
	err     error
}

func (f *fixedBroadcasts) Broadcast(_ context.Context, command BroadcastCommand) (BroadcastResult, error) {
	f.command = command
	return f.result, f.err
}

func TestBroadcastIsValidatedAndAudited(t *testing.T) {
	app, audit := newTestApp(t)
	broadcasts := &fixedBroadcasts{result: BroadcastResult{Broadcast: true, RecipientCount: 4}}
	app.broadcasts = broadcasts
	request := authenticatedRequest(t, app, http.MethodPost, "/api/v1/broadcasts")
	request.Body = io.NopCloser(bytes.NewBufferString(`{"message":"  Maintenance in ten minutes  "}`))
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || broadcasts.command.Message != "Maintenance in ten minutes" {
		t.Fatalf("response=%d %s command=%#v", response.Code, response.Body.String(), broadcasts.command)
	}
	if len(audit.events) != 2 || audit.events[0].Outcome != "attempt" || audit.events[1].Outcome != "success" || audit.events[1].Details["recipientCount"] != 4 {
		t.Fatalf("audit events = %#v", audit.events)
	}
	if _, mutated := audit.events[0].Details["recipientCount"]; mutated {
		t.Fatalf("attempt audit was mutated: %#v", audit.events[0])
	}
}

func TestAuditEventsAreAuthenticatedFilteredAndBounded(t *testing.T) {
	app, audit := newTestApp(t)
	audit.events = []AuditEvent{
		{Action: "operator.login", Outcome: "success"},
		{Action: "world.broadcast", Outcome: "success"},
	}
	request := authenticatedRequest(t, app, http.MethodGet, "/api/v1/audit-events?action=world.broadcast&limit=1")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"action":"world.broadcast"`)) || bytes.Contains(response.Body.Bytes(), []byte(`"operator.login"`)) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	invalid := authenticatedRequest(t, app, http.MethodGet, "/api/v1/audit-events?action=bad%20action")
	invalidResponse := httptest.NewRecorder()
	app.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}
