package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/framework/security/mtls"
	smcconfig "github.com/Harvi8/AegisCore/smc-server/config"
)

const agentOnlinePlayersPath = "/v1/management/players"
const agentPlayerDisconnectsPath = "/v1/management/player-disconnects"
const agentBroadcastsPath = "/v1/management/broadcasts"

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
