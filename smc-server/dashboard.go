package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sync"
	"time"
)

type ServiceHealth struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	State     string `json:"state"`
	LatencyMS int64  `json:"latencyMs"`
}

type ShardSummary struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	ContentID     int    `json:"contentId"`
	Capacity      int    `json:"capacity"`
	OnlinePlayers int    `json:"onlinePlayers"`
	Status        int    `json:"status"`
}

type DashboardTotals struct {
	Accounts       int64 `json:"accounts"`
	ActiveAccounts int64 `json:"activeAccounts"`
	GameMasters    int64 `json:"gameMasters"`
	Characters     int64 `json:"characters"`
	Guilds         int64 `json:"guilds"`
	Items          int64 `json:"items"`
	Notices        int64 `json:"notices"`
}

type DashboardSnapshot struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Totals      DashboardTotals `json:"totals"`
	Services    []ServiceHealth `json:"services"`
	Shards      []ShardSummary  `json:"shards"`
}

type DashboardSource interface {
	Snapshot(context.Context) (DashboardSnapshot, error)
}

type DashboardRepository struct {
	account        *sql.DB
	shard          *sql.DB
	gatewayAddress string
	agentAddress   string
	probeTimeout   time.Duration
}

func NewDashboardRepository(account, shard *sql.DB, gatewayAddress, agentAddress string) (*DashboardRepository, error) {
	if account == nil || shard == nil {
		return nil, fmt.Errorf("SMC dashboard requires account and shard database pools")
	}
	return &DashboardRepository{
		account: account, shard: shard,
		gatewayAddress: gatewayAddress, agentAddress: agentAddress,
		probeTimeout: 750 * time.Millisecond,
	}, nil
}

func (r *DashboardRepository) Snapshot(ctx context.Context) (DashboardSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := DashboardSnapshot{GeneratedAt: time.Now().UTC()}
	const accountTotalsQuery = "SELECT COUNT(*), " +
		"COALESCE(SUM(CASE WHEN STATUS = 1 THEN 1 ELSE 0 END), 0), " +
		"COALESCE(SUM(CASE WHEN IS_GM = 1 THEN 1 ELSE 0 END), 0) " +
		"FROM `USER`"
	if err := r.account.QueryRowContext(ctx, accountTotalsQuery).Scan(
		&snapshot.Totals.Accounts,
		&snapshot.Totals.ActiveAccounts,
		&snapshot.Totals.GameMasters,
	); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC account totals: %w", err)
	}
	if err := r.account.QueryRowContext(ctx, `SELECT COUNT(*) FROM NOTICE`).Scan(&snapshot.Totals.Notices); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC notice total: %w", err)
	}
	rows, err := r.account.QueryContext(ctx, `
		SELECT ID, NAME, CONTENT_ID, CAPACITY, ONLINE_PLAYERS, STATUS
		FROM SHARD ORDER BY ID`)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC shards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ShardSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.ContentID, &item.Capacity, &item.OnlinePlayers, &item.Status); err != nil {
			return DashboardSnapshot{}, fmt.Errorf("scan SMC shard: %w", err)
		}
		snapshot.Shards = append(snapshot.Shards, item)
	}
	if err := rows.Err(); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("iterate SMC shards: %w", err)
	}
	if err := r.shard.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM `+"`CHAR`"+`),
		       (SELECT COUNT(*) FROM GUILD),
		       (SELECT COUNT(*) FROM ITEM)`).Scan(
		&snapshot.Totals.Characters,
		&snapshot.Totals.Guilds,
		&snapshot.Totals.Items,
	); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC shard totals: %w", err)
	}

	snapshot.Services = make([]ServiceHealth, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		snapshot.Services[0] = r.probe("gateway", "Gateway", r.gatewayAddress)
	}()
	go func() {
		defer wait.Done()
		snapshot.Services[1] = r.probe("agent", "Agent", r.agentAddress)
	}()
	wait.Wait()
	return snapshot, nil
}

func (r *DashboardRepository) probe(id, name, address string) ServiceHealth {
	started := time.Now()
	connection, err := net.DialTimeout("tcp", address, r.probeTimeout)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return ServiceHealth{ID: id, Name: name, Address: address, State: "offline", LatencyMS: latency}
	}
	_ = connection.Close()
	return ServiceHealth{ID: id, Name: name, Address: address, State: "reachable", LatencyMS: latency}
}
