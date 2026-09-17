package config

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/feature"
	"github.com/spf13/viper"
)

func TestAgentDefaults(t *testing.T) {
	resetViper(t)
	setDefaultValues()

	assertConfigValue(t, AgentHost, "127.0.0.1")
	assertConfigValue(t, AgentPort, 15882)
	assertConfigValue(t, AgentModuleId, "AgentServer")
	assertConfigValue(t, AgentDataPath, "./Data")
	assertConfigValue(t, AgentPrelinkedNavdataFile, "./prelinked_navdata.gob")
	assertConfigValue(t, AgentTrafficPacketLimit, 200)
	assertConfigValue(t, AgentTrafficWindowMS, 1000)
	assertConfigValue(t, AgentTimedBuffDuration, false)
	assertConfigValue(t, AgentGiantMonsterSpawnRatio, 1000)
	assertConfigValue(t, AgentBattleArenaTicketDropRate, 0)
	assertConfigValue(t, AgentDiagnosticsEnabled, false)
	assertConfigValue(t, AgentDiagnosticsPort, 6060)
	assertConfigValue(t, AgentManagementEnabled, false)
	assertConfigValue(t, AgentManagementHost, "127.0.0.1")
	assertConfigValue(t, AgentManagementPort, 15884)
	assertConfigValue(t, AgentSchedulerEnabled, true)
	assertConfigValue(t, AgentSchedulerPollMS, 1000)
	assertConfigValue(t, AgentSchedulerLeaseSeconds, 30)
	assertConfigValue(t, AgentSchedulerBatchSize, 64)
	assertConfigValue(t, AgentCompetitiveMinimumPlayers, 2)
	assertConfigValue(t, AgentCompetitiveMaximumPlayers, 16)
	assertConfigValue(t, AgentCompetitiveMatchSeconds, 600)
	assertConfigValue(t, AgentCompetitiveWinnerGold, uint64(1_000_000))
	assertConfigValue(t, AgentCompetitiveLoserGold, uint64(250_000))
	assertConfigValue(t, AgentAdmissionHost, "127.0.0.1")
	assertConfigValue(t, AgentAdmissionPort, 15883)
	assertConfigValue(t, GameTimeTicksPerSecond, 20)
	assertConfigValue(t, GameTimeDaySpeed, 1.0)
	assertConfigValue(t, PositionCheckpointSeconds, 30)
	for _, definition := range feature.Definitions() {
		assertConfigValue(t, definition.ConfigKey, false)
	}

	for _, key := range []string{
		AgentAdmissionTLSCertFile, AgentAdmissionTLSKeyFile, AgentAdmissionTLSClientCAFile,
		AgentManagementTLSCertFile, AgentManagementTLSKeyFile, AgentManagementTLSClientCAFile,
	} {
		if viper.IsSet(key) {
			t.Fatalf("%s must not have an insecure default", key)
		}
	}
}

func TestAgentEnvironmentBindingsConvertRuntimeValues(t *testing.T) {
	resetViper(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("AGENT_HOST", "agent-server")
	t.Setenv("AGENT_PORT", "16000")
	t.Setenv("AGENT_ADMISSION_PORT", "16001")
	t.Setenv("AGENT_TRAFFIC_FILTER_PACKET_LIMIT", "250")
	t.Setenv("AGENT_TIMED_BUFF_DURATION", "1")
	t.Setenv("AGENT_GIANT_MONSTER_SPAWN_RATIO", "750")
	t.Setenv("BATTLE_ARENA_TICKET_DROP_RATE", "25")
	t.Setenv("AGENT_DIAGNOSTICS_ENABLED", "1")
	t.Setenv("AGENT_DIAGNOSTICS_PORT", "6061")
	t.Setenv("AGENT_MANAGEMENT_ENABLED", "1")
	t.Setenv("AGENT_MANAGEMENT_PORT", "16002")
	t.Setenv("AGENT_MANAGEMENT_TLS_CERT_FILE", "/tls/management-agent.pem")
	t.Setenv("AGENT_SCHEDULER_ENABLED", "0")
	t.Setenv("AGENT_SCHEDULER_POLL_MS", "250")
	t.Setenv("AGENT_SCHEDULER_LEASE_SECONDS", "45")
	t.Setenv("AGENT_SCHEDULER_BATCH_SIZE", "32")
	t.Setenv("AGENT_COMPETITIVE_MINIMUM_PLAYERS", "4")
	t.Setenv("AGENT_COMPETITIVE_MAXIMUM_PLAYERS", "24")
	t.Setenv("AGENT_COMPETITIVE_MATCH_SECONDS", "900")
	t.Setenv("AGENT_COMPETITIVE_WINNER_GOLD", "2000000")
	t.Setenv("AGENT_COMPETITIVE_LOSER_GOLD", "500000")
	t.Setenv("GAME_TIME_TICKS_PER_SECOND", "20")
	t.Setenv("GAME_TIME_DAY_SPEED", "1.5")
	t.Setenv("AGENT_POSITION_CHECKPOINT_SECONDS", "45")
	t.Setenv("AGENT_ADMISSION_TLS_CERT_FILE", "/tls/agent.pem")
	t.Setenv("AGENT_FEATURE_OPEN_MARKET_ENABLED", "1")

	assertConfigValue(t, AgentHost, "agent-server")
	if got := viper.GetInt(AgentPort); got != 16000 {
		t.Fatalf("agent port = %d, want 16000", got)
	}
	if got := viper.GetInt(AgentAdmissionPort); got != 16001 {
		t.Fatalf("admission port = %d, want 16001", got)
	}
	if got := viper.GetUint(AgentTrafficPacketLimit); got != 250 {
		t.Fatalf("packet limit = %d, want 250", got)
	}
	if !viper.GetBool(AgentTimedBuffDuration) {
		t.Fatal("timed buff duration extension was not enabled from environment")
	}
	if got := viper.GetInt(AgentGiantMonsterSpawnRatio); got != 750 {
		t.Fatalf("giant monster spawn ratio = %d, want 750", got)
	}
	if got := viper.GetInt(AgentBattleArenaTicketDropRate); got != 25 {
		t.Fatalf("Battle Arena ticket rate = %d, want 25", got)
	}
	if !viper.GetBool(AgentDiagnosticsEnabled) {
		t.Fatal("Agent diagnostics were not enabled from environment")
	}
	if got := viper.GetInt(AgentDiagnosticsPort); got != 6061 {
		t.Fatalf("diagnostics port = %d, want 6061", got)
	}
	if !viper.GetBool(AgentManagementEnabled) || viper.GetInt(AgentManagementPort) != 16002 {
		t.Fatal("Agent management environment was not bound")
	}
	assertConfigValue(t, AgentManagementTLSCertFile, "/tls/management-agent.pem")
	if viper.GetBool(AgentSchedulerEnabled) || viper.GetInt(AgentSchedulerPollMS) != 250 || viper.GetInt(AgentSchedulerLeaseSeconds) != 45 || viper.GetInt(AgentSchedulerBatchSize) != 32 {
		t.Fatal("Agent scheduler environment was not bound")
	}
	if viper.GetInt(AgentCompetitiveMinimumPlayers) != 4 || viper.GetInt(AgentCompetitiveMaximumPlayers) != 24 || viper.GetInt(AgentCompetitiveMatchSeconds) != 900 || viper.GetUint64(AgentCompetitiveWinnerGold) != 2_000_000 || viper.GetUint64(AgentCompetitiveLoserGold) != 500_000 {
		t.Fatal("competitive runtime environment was not bound")
	}
	if got := viper.GetInt(GameTimeTicksPerSecond); got != 20 {
		t.Fatalf("ticks per second = %d, want 20", got)
	}
	if got := viper.GetFloat64(GameTimeDaySpeed); got != 1.5 {
		t.Fatalf("day speed = %v, want 1.5", got)
	}
	if got := viper.GetInt(PositionCheckpointSeconds); got != 45 {
		t.Fatalf("checkpoint seconds = %d, want 45", got)
	}
	assertConfigValue(t, AgentAdmissionTLSCertFile, "/tls/agent.pem")
	if !viper.GetBool("agent.features.open_market.enabled") {
		t.Fatal("open-market feature environment was not bound")
	}
	if _, err := ResolveFeatures(); err == nil {
		t.Fatal("unavailable open-market feature was accepted")
	}
}

func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
}

func assertConfigValue(t *testing.T, key string, want any) {
	t.Helper()
	if got := viper.Get(key); got != want {
		t.Fatalf("viper.Get(%q) = %#v, want %#v", key, got, want)
	}
}
