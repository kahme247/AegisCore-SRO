package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/framework/scheduler"
	"github.com/Harvi8/AegisCore/framework/security/mtls"
	smcconfig "github.com/Harvi8/AegisCore/smc-server/config"
)

const agentOnlinePlayersPath = "/v1/management/players"
const agentPlayerDisconnectsPath = "/v1/management/player-disconnects"
const agentBroadcastsPath = "/v1/management/broadcasts"
const agentSchedulesPath = "/v1/management/schedules"

var (
	ErrAgentPlayerNotOnline       = errors.New("player is not online in Agent")
	ErrAgentPlayerIdentityChanged = errors.New("player runtime identity changed")
)

type LivePlayerRecord struct {
	CharacterID int       `json:"characterId"`
	UniqueID    uint32    `json:"uniqueId"`
	Name        string    `json:"name"`
	Level       int       `json:"level"`
	Gold        uint64    `json:"gold"`
	HP          int       `json:"hp"`
	MP          int       `json:"mp"`
	RegionID    int16     `json:"regionId"`
	PositionX   float32   `json:"positionX"`
	PositionY   float32   `json:"positionY"`
	PositionZ   float32   `json:"positionZ"`
	GameMaster  bool      `json:"gameMaster"`
	LastSeen    time.Time `json:"lastSeen"`
}

type LivePlayerSnapshot struct {
	GeneratedAt time.Time          `json:"generatedAt"`
	Players     []LivePlayerRecord `json:"players"`
}

type LivePlayerSource interface {
	OnlinePlayers(context.Context) (LivePlayerSnapshot, error)
}

type PlayerDisconnectCommand struct {
	CharacterID      int    `json:"characterId"`
	ExpectedUniqueID uint32 `json:"expectedUniqueId"`
	Reason           string `json:"reason"`
}

type PlayerDisconnectResult struct {
	Disconnected bool   `json:"disconnected"`
	CharacterID  int    `json:"characterId"`
	UniqueID     uint32 `json:"uniqueId"`
	Name         string `json:"name"`
}

type PlayerDisconnectSource interface {
	DisconnectPlayer(context.Context, PlayerDisconnectCommand) (PlayerDisconnectResult, error)
}

type BroadcastCommand struct {
	Message string `json:"message"`
}

type BroadcastResult struct {
	Broadcast      bool `json:"broadcast"`
	RecipientCount int  `json:"recipientCount"`
}

type BroadcastSource interface {
	Broadcast(context.Context, BroadcastCommand) (BroadcastResult, error)
}

type ScheduleSource interface {
	ListSchedules(context.Context) ([]scheduler.Definition, error)
	GetSchedule(context.Context, string) (scheduler.Definition, error)
	CreateSchedule(context.Context, scheduler.Definition) (scheduler.Definition, error)
	SetScheduleEnabled(context.Context, string, uint64, bool) (scheduler.Definition, error)
	ScheduleRuns(context.Context, string, int) ([]scheduler.RunRecord, error)
}

func (c *AgentManagementClient) SetScheduleEnabled(ctx context.Context, id string, expectedVersion uint64, enabled bool) (scheduler.Definition, error) {
	endpoint, err := c.managementEndpoint(agentSchedulesPath + "/" + url.PathEscape(id))
	if err != nil {
		return scheduler.Definition{}, err
	}
	payload, _ := json.Marshal(struct {
		ExpectedVersion uint64 `json:"expectedVersion"`
		Enabled         bool   `json:"enabled"`
	}{expectedVersion, enabled})
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(payload))
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("build Agent schedule-update request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("request Agent schedule update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return scheduler.Definition{}, fmt.Errorf("Agent schedule update returned status %d", response.StatusCode)
	}
	var definition scheduler.Definition
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&definition); err != nil {
		return scheduler.Definition{}, fmt.Errorf("decode Agent schedule update: %w", err)
	}
	return definition, nil
}

func (c *AgentManagementClient) ScheduleRuns(ctx context.Context, id string, limit int) ([]scheduler.RunRecord, error) {
	endpoint, err := c.managementEndpoint(agentSchedulesPath + "/" + url.PathEscape(id) + "/runs?limit=" + fmt.Sprintf("%d", limit))
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build Agent schedule-history request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Agent schedule history: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Agent schedule history returned status %d", response.StatusCode)
	}
	var result struct {
		Runs []scheduler.RunRecord `json:"runs"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Agent schedule history: %w", err)
	}
	if result.Runs == nil {
		result.Runs = []scheduler.RunRecord{}
	}
	return result.Runs, nil
}

type AgentManagementClient struct {
	endpoint string
	client   *http.Client
}

func NewAgentManagementClient(settings smcconfig.Settings) (*AgentManagementClient, error) {
	tlsConfig, err := mtls.LoadClientConfig(mtls.Files{
		CertFile:   settings.AgentManagementTLSCertFile,
		KeyFile:    settings.AgentManagementTLSKeyFile,
		CAFile:     settings.AgentManagementTLSCAFile,
		ServerName: settings.AgentManagementTLSServerName,
	})
	if err != nil {
		return nil, fmt.Errorf("configure SMC Agent management mTLS client: %w", err)
	}
	return &AgentManagementClient{
		endpoint: strings.TrimRight(settings.AgentManagementEndpoint, "/") + agentOnlinePlayersPath,
		client: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second},
			Timeout:   3 * time.Second,
		},
	}, nil
}

func (c *AgentManagementClient) OnlinePlayers(ctx context.Context) (LivePlayerSnapshot, error) {
	if c == nil || c.client == nil || c.endpoint == "" {
		return LivePlayerSnapshot{}, errors.New("Agent management client is unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return LivePlayerSnapshot{}, fmt.Errorf("build Agent online-player request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return LivePlayerSnapshot{}, fmt.Errorf("request Agent online-player snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return LivePlayerSnapshot{}, fmt.Errorf("Agent online-player snapshot returned status %d", response.StatusCode)
	}
	var snapshot LivePlayerSnapshot
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&snapshot); err != nil {
		return LivePlayerSnapshot{}, fmt.Errorf("decode Agent online-player snapshot: %w", err)
	}
	if snapshot.Players == nil {
		snapshot.Players = []LivePlayerRecord{}
	}
	return snapshot, nil
}

func (c *AgentManagementClient) DisconnectPlayer(ctx context.Context, command PlayerDisconnectCommand) (PlayerDisconnectResult, error) {
	if c == nil || c.client == nil || c.endpoint == "" {
		return PlayerDisconnectResult{}, errors.New("Agent management client is unavailable")
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return PlayerDisconnectResult{}, fmt.Errorf("encode Agent disconnect command: %w", err)
	}
	endpoint := strings.TrimSuffix(c.endpoint, agentOnlinePlayersPath) + agentPlayerDisconnectsPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return PlayerDisconnectResult{}, fmt.Errorf("build Agent disconnect request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return PlayerDisconnectResult{}, fmt.Errorf("request Agent player disconnect: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return PlayerDisconnectResult{}, ErrAgentPlayerNotOnline
	case http.StatusConflict:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return PlayerDisconnectResult{}, ErrAgentPlayerIdentityChanged
	case http.StatusOK:
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return PlayerDisconnectResult{}, fmt.Errorf("Agent player disconnect returned status %d", response.StatusCode)
	}
	var result PlayerDisconnectResult
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return PlayerDisconnectResult{}, fmt.Errorf("decode Agent player disconnect: %w", err)
	}
	if !result.Disconnected || result.CharacterID != command.CharacterID || result.UniqueID != command.ExpectedUniqueID {
		return PlayerDisconnectResult{}, errors.New("Agent player disconnect acknowledgement did not match command")
	}
	return result, nil
}

func (c *AgentManagementClient) Broadcast(ctx context.Context, command BroadcastCommand) (BroadcastResult, error) {
	if c == nil || c.client == nil || c.endpoint == "" {
		return BroadcastResult{}, errors.New("Agent management client is unavailable")
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("encode Agent broadcast command: %w", err)
	}
	endpoint := strings.TrimSuffix(c.endpoint, agentOnlinePlayersPath) + agentBroadcastsPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("build Agent broadcast request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("request Agent broadcast: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return BroadcastResult{}, fmt.Errorf("Agent broadcast returned status %d", response.StatusCode)
	}
	var result BroadcastResult
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return BroadcastResult{}, fmt.Errorf("decode Agent broadcast: %w", err)
	}
	if !result.Broadcast || result.RecipientCount < 0 {
		return BroadcastResult{}, errors.New("Agent broadcast acknowledgement was invalid")
	}
	return result, nil
}

func (c *AgentManagementClient) ListSchedules(ctx context.Context) ([]scheduler.Definition, error) {
	endpoint, err := c.managementEndpoint(agentSchedulesPath)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build Agent schedule-list request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Agent schedule list: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Agent schedule list returned status %d", response.StatusCode)
	}
	var result struct {
		Schedules []scheduler.Definition `json:"schedules"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Agent schedule list: %w", err)
	}
	if result.Schedules == nil {
		result.Schedules = []scheduler.Definition{}
	}
	return result.Schedules, nil
}

func (c *AgentManagementClient) GetSchedule(ctx context.Context, id string) (scheduler.Definition, error) {
	endpoint, err := c.managementEndpoint(agentSchedulesPath + "/" + url.PathEscape(id))
	if err != nil {
		return scheduler.Definition{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("build Agent schedule request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("request Agent schedule: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return scheduler.Definition{}, fmt.Errorf("Agent schedule returned status %d", response.StatusCode)
	}
	var definition scheduler.Definition
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&definition); err != nil {
		return scheduler.Definition{}, fmt.Errorf("decode Agent schedule: %w", err)
	}
	return definition, nil
}

func (c *AgentManagementClient) CreateSchedule(ctx context.Context, definition scheduler.Definition) (scheduler.Definition, error) {
	endpoint, err := c.managementEndpoint(agentSchedulesPath)
	if err != nil {
		return scheduler.Definition{}, err
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("encode Agent schedule: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("build Agent schedule-create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return scheduler.Definition{}, fmt.Errorf("request Agent schedule creation: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return scheduler.Definition{}, fmt.Errorf("Agent schedule creation returned status %d", response.StatusCode)
	}
	var created scheduler.Definition
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&created); err != nil {
		return scheduler.Definition{}, fmt.Errorf("decode Agent schedule creation: %w", err)
	}
	return created, nil
}

func (c *AgentManagementClient) managementEndpoint(path string) (string, error) {
	if c == nil || c.client == nil || c.endpoint == "" {
		return "", errors.New("Agent management client is unavailable")
	}
	return strings.TrimSuffix(c.endpoint, agentOnlinePlayersPath) + path, nil
}
