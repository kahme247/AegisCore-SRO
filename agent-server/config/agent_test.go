package config

import (
	"testing"

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
	assertConfigValue(t, AgentLevelDataFile, "./Media/server_dep/silkroad/textdata/leveldata.txt")
	assertConfigValue(t, AgentTrafficPacketLimit, 200)
	assertConfigValue(t, AgentTrafficWindowMS, 1000)
	assertConfigValue(t, AgentTimedBuffDuration, false)
	assertConfigValue(t, AgentDiagnosticsEnabled, false)
	assertConfigValue(t, AgentDiagnosticsPort, 6060)
	assertConfigValue(t, AgentAdmissionHost, "127.0.0.1")
	assertConfigValue(t, AgentAdmissionPort, 15883)
	assertConfigValue(t, GameTimeTicksPerSecond, 20)
	assertConfigValue(t, GameTimeDaySpeed, 1.0)
	assertConfigValue(t, PositionCheckpointSeconds, 30)

	for _, key := range []string{AgentAdmissionTLSCertFile, AgentAdmissionTLSKeyFile, AgentAdmissionTLSClientCAFile} {
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
	t.Setenv("AGENT_DIAGNOSTICS_ENABLED", "1")
	t.Setenv("AGENT_DIAGNOSTICS_PORT", "6061")
	t.Setenv("GAME_TIME_TICKS_PER_SECOND", "20")
	t.Setenv("GAME_TIME_DAY_SPEED", "1.5")
	t.Setenv("AGENT_POSITION_CHECKPOINT_SECONDS", "45")
	t.Setenv("AGENT_ADMISSION_TLS_CERT_FILE", "/tls/agent.pem")

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
	if !viper.GetBool(AgentDiagnosticsEnabled) {
		t.Fatal("Agent diagnostics were not enabled from environment")
	}
	if got := viper.GetInt(AgentDiagnosticsPort); got != 6061 {
		t.Fatalf("diagnostics port = %d, want 6061", got)
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
