package config

import (
	"github.com/Harvi8/AegisCore/agent-server/feature"
	"github.com/Harvi8/AegisCore/framework/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	AgentHost                      = "agent.host"
	AgentPort                      = "agent.port"
	AgentModuleId                  = "agent.module_id"
	AgentDataPath                  = "agent.data_path"
	AgentDungeonTeleportDataPath   = "agent.dungeon.teleport_data_path"
	AgentPrelinkedNavdataFile      = "agent.prelinked_navdata_file"
	AgentTrafficPacketLimit        = "agent.traffic_filter.packet_limit"
	AgentTrafficWindowMS           = "agent.traffic_filter.window_ms"
	AgentTimedBuffDuration         = "agent.client.timed_buff_duration"
	AgentGiantMonsterSpawnRatio    = "agent.monsters.giant_spawn_ratio"
	AgentTradeSafetyOption         = "agent.trade.safety_option"
	AgentTradeSafetyNum            = "agent.trade.safety_num"
	AgentBattleArenaTicketDropRate = "agent.events.battle_arena.ticket_drop_rate"
	AgentDiagnosticsEnabled        = "agent.diagnostics.enabled"
	AgentDiagnosticsPort           = "agent.diagnostics.port"
	AgentManagementEnabled         = "agent.management.enabled"
	AgentManagementHost            = "agent.management.host"
	AgentManagementPort            = "agent.management.port"
	AgentManagementTLSCertFile     = "agent.management.tls.cert_file"
	AgentManagementTLSKeyFile      = "agent.management.tls.key_file"
	AgentManagementTLSClientCAFile = "agent.management.tls.client_ca_file"
	AgentSchedulerEnabled          = "agent.scheduler.enabled"
	AgentSchedulerPollMS           = "agent.scheduler.poll_ms"
	AgentSchedulerLeaseSeconds     = "agent.scheduler.lease_seconds"
	AgentSchedulerBatchSize        = "agent.scheduler.batch_size"
	AgentCompetitiveMinimumPlayers = "agent.competitive.minimum_players"
	AgentCompetitiveMaximumPlayers = "agent.competitive.maximum_players"
	AgentCompetitiveMatchSeconds   = "agent.competitive.match_seconds"
	AgentCompetitiveWinnerGold     = "agent.competitive.winner_gold"
	AgentCompetitiveLoserGold      = "agent.competitive.loser_gold"

	// AgentAdmission* configures the private Gateway-to-Agent mTLS listener.
	// Certificate paths have no defaults so an Agent cannot start an insecure
	// fallback channel.
	AgentAdmissionHost            = "agent.admission.host"
	AgentAdmissionPort            = "agent.admission.port"
	AgentAdmissionTLSCertFile     = "agent.admission.tls.cert_file"
	AgentAdmissionTLSKeyFile      = "agent.admission.tls.key_file"
	AgentAdmissionTLSClientCAFile = "agent.admission.tls.client_ca_file"

	GameTimeTicksPerSecond    = "game.time.ticks_per_second"
	GameTimeDaySpeed          = "game.time.day_speed"
	PositionCheckpointSeconds = "position.checkpoint_seconds"
)

func Initialize() {
	config.Initialize()

	setDefaultValues()
	bindEnvAliases()

	logrus.Info("agent config initialized")
}

func bindEnvAliases() {
	viper.BindEnv(AgentHost, "AGENT_HOST")
	viper.BindEnv(AgentPort, "AGENT_PORT")
	viper.BindEnv(AgentModuleId, "AGENT_MODULE_ID")
	viper.BindEnv(AgentDataPath, "AGENT_DATA_PATH")
	viper.BindEnv(AgentDungeonTeleportDataPath, "AGENT_DUNGEON_TELEPORT_DATA_PATH")
	viper.BindEnv(AgentPrelinkedNavdataFile, "AGENT_PRELINKED_NAVDATA_FILE")
	viper.BindEnv(AgentTrafficPacketLimit, "AGENT_TRAFFIC_FILTER_PACKET_LIMIT")
	viper.BindEnv(AgentTrafficWindowMS, "AGENT_TRAFFIC_FILTER_WINDOW_MS")
	viper.BindEnv(AgentTimedBuffDuration, "AGENT_TIMED_BUFF_DURATION")
	viper.BindEnv(AgentGiantMonsterSpawnRatio, "AGENT_GIANT_MONSTER_SPAWN_RATIO")
	viper.BindEnv(AgentTradeSafetyOption, "AGENT_TRADE_SAFETY_OPTION")
	viper.BindEnv(AgentTradeSafetyNum, "AGENT_TRADE_SAFETY_NUM")
	viper.BindEnv(AgentBattleArenaTicketDropRate, "BATTLE_ARENA_TICKET_DROP_RATE")
	viper.BindEnv(AgentDiagnosticsEnabled, "AGENT_DIAGNOSTICS_ENABLED")
	viper.BindEnv(AgentDiagnosticsPort, "AGENT_DIAGNOSTICS_PORT")
	viper.BindEnv(AgentManagementEnabled, "AGENT_MANAGEMENT_ENABLED")
	viper.BindEnv(AgentManagementHost, "AGENT_MANAGEMENT_HOST")
	viper.BindEnv(AgentManagementPort, "AGENT_MANAGEMENT_PORT")
	viper.BindEnv(AgentManagementTLSCertFile, "AGENT_MANAGEMENT_TLS_CERT_FILE")
	viper.BindEnv(AgentManagementTLSKeyFile, "AGENT_MANAGEMENT_TLS_KEY_FILE")
	viper.BindEnv(AgentManagementTLSClientCAFile, "AGENT_MANAGEMENT_TLS_CLIENT_CA_FILE")
	viper.BindEnv(AgentSchedulerEnabled, "AGENT_SCHEDULER_ENABLED")
	viper.BindEnv(AgentSchedulerPollMS, "AGENT_SCHEDULER_POLL_MS")
	viper.BindEnv(AgentSchedulerLeaseSeconds, "AGENT_SCHEDULER_LEASE_SECONDS")
	viper.BindEnv(AgentSchedulerBatchSize, "AGENT_SCHEDULER_BATCH_SIZE")
	viper.BindEnv(AgentCompetitiveMinimumPlayers, "AGENT_COMPETITIVE_MINIMUM_PLAYERS")
	viper.BindEnv(AgentCompetitiveMaximumPlayers, "AGENT_COMPETITIVE_MAXIMUM_PLAYERS")
	viper.BindEnv(AgentCompetitiveMatchSeconds, "AGENT_COMPETITIVE_MATCH_SECONDS")
	viper.BindEnv(AgentCompetitiveWinnerGold, "AGENT_COMPETITIVE_WINNER_GOLD")
	viper.BindEnv(AgentCompetitiveLoserGold, "AGENT_COMPETITIVE_LOSER_GOLD")
	viper.BindEnv(AgentAdmissionHost, "AGENT_ADMISSION_HOST")
	viper.BindEnv(AgentAdmissionPort, "AGENT_ADMISSION_PORT")
	viper.BindEnv(AgentAdmissionTLSCertFile, "AGENT_ADMISSION_TLS_CERT_FILE")
	viper.BindEnv(AgentAdmissionTLSKeyFile, "AGENT_ADMISSION_TLS_KEY_FILE")
	viper.BindEnv(AgentAdmissionTLSClientCAFile, "AGENT_ADMISSION_TLS_CLIENT_CA_FILE")

	viper.BindEnv(GameTimeTicksPerSecond, "GAME_TIME_TICKS_PER_SECOND")
	viper.BindEnv(GameTimeDaySpeed, "GAME_TIME_DAY_SPEED")
	viper.BindEnv(PositionCheckpointSeconds, "AGENT_POSITION_CHECKPOINT_SECONDS")
	for _, definition := range feature.Definitions() {
		viper.BindEnv(definition.ConfigKey, definition.Environment)
	}
}

func setDefaultValues() {
	viper.SetDefault(AgentHost, "127.0.0.1")
	viper.SetDefault(AgentPort, 15882)
	viper.SetDefault(AgentModuleId, "AgentServer")
	viper.SetDefault(AgentDataPath, "./Data")
	viper.SetDefault(AgentPrelinkedNavdataFile, "./prelinked_navdata.gob")
	viper.SetDefault(AgentTrafficPacketLimit, 200)
	viper.SetDefault(AgentTrafficWindowMS, 1000)
	viper.SetDefault(AgentTimedBuffDuration, false)
	viper.SetDefault(AgentGiantMonsterSpawnRatio, 1000)
	// Pinned vSRO 1.188 _RefShardContentConfig values.
	viper.SetDefault(AgentTradeSafetyOption, 0)
	viper.SetDefault(AgentTradeSafetyNum, 8)
	// All nine Arena groups in the supplied gameworldconfigdata.txt author 0.
	viper.SetDefault(AgentBattleArenaTicketDropRate, 0)
	viper.SetDefault(AgentDiagnosticsEnabled, false)
	viper.SetDefault(AgentDiagnosticsPort, 6060)
	viper.SetDefault(AgentManagementEnabled, false)
	viper.SetDefault(AgentManagementHost, "127.0.0.1")
	viper.SetDefault(AgentManagementPort, 15884)
	viper.SetDefault(AgentSchedulerEnabled, true)
	viper.SetDefault(AgentSchedulerPollMS, 1000)
	viper.SetDefault(AgentSchedulerLeaseSeconds, 30)
	viper.SetDefault(AgentSchedulerBatchSize, 64)
	viper.SetDefault(AgentCompetitiveMinimumPlayers, 2)
	viper.SetDefault(AgentCompetitiveMaximumPlayers, 16)
	viper.SetDefault(AgentCompetitiveMatchSeconds, 600)
	viper.SetDefault(AgentCompetitiveWinnerGold, uint64(1_000_000))
	viper.SetDefault(AgentCompetitiveLoserGold, uint64(250_000))
	// A bare Agent defaults to loopback. Container deployments explicitly bind
	// this to their private service network through AGENT_ADMISSION_HOST.
	viper.SetDefault(AgentAdmissionHost, "127.0.0.1")
	viper.SetDefault(AgentAdmissionPort, 15883)

	viper.SetDefault(GameTimeTicksPerSecond, 20)
	viper.SetDefault(GameTimeDaySpeed, 1.0)
	viper.SetDefault(PositionCheckpointSeconds, 30)
	for _, definition := range feature.Definitions() {
		viper.SetDefault(definition.ConfigKey, definition.DefaultEnabled)
	}
}

func ResolveFeatures() (feature.Set, error) {
	return feature.Resolve(viperBoolProvider{})
}

type viperBoolProvider struct{}

func (viperBoolProvider) GetBool(key string) bool { return viper.GetBool(key) }
