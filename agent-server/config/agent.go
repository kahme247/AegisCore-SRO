package config

import (
	"github.com/ferdoran/go-sro/framework/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	AgentHost                 = "agent.host"
	AgentPort                 = "agent.port"
	AgentModuleId             = "agent.module_id"
	AgentDataPath             = "agent.data_path"
	AgentPrelinkedNavdataFile = "agent.prelinked_navdata_file"
	AgentLevelDataFile        = "agent.level_data_file"
	AgentTrafficPacketLimit   = "agent.traffic_filter.packet_limit"
	AgentTrafficWindowMS      = "agent.traffic_filter.window_ms"
	AgentTimedBuffDuration    = "agent.client.timed_buff_duration"
	AgentDiagnosticsEnabled   = "agent.diagnostics.enabled"
	AgentDiagnosticsPort      = "agent.diagnostics.port"

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
	viper.BindEnv(AgentPrelinkedNavdataFile, "AGENT_PRELINKED_NAVDATA_FILE")
	viper.BindEnv(AgentLevelDataFile, "AGENT_LEVEL_DATA_FILE")
	viper.BindEnv(AgentTrafficPacketLimit, "AGENT_TRAFFIC_FILTER_PACKET_LIMIT")
	viper.BindEnv(AgentTrafficWindowMS, "AGENT_TRAFFIC_FILTER_WINDOW_MS")
	viper.BindEnv(AgentTimedBuffDuration, "AGENT_TIMED_BUFF_DURATION")
	viper.BindEnv(AgentDiagnosticsEnabled, "AGENT_DIAGNOSTICS_ENABLED")
	viper.BindEnv(AgentDiagnosticsPort, "AGENT_DIAGNOSTICS_PORT")
	viper.BindEnv(AgentAdmissionHost, "AGENT_ADMISSION_HOST")
	viper.BindEnv(AgentAdmissionPort, "AGENT_ADMISSION_PORT")
	viper.BindEnv(AgentAdmissionTLSCertFile, "AGENT_ADMISSION_TLS_CERT_FILE")
	viper.BindEnv(AgentAdmissionTLSKeyFile, "AGENT_ADMISSION_TLS_KEY_FILE")
	viper.BindEnv(AgentAdmissionTLSClientCAFile, "AGENT_ADMISSION_TLS_CLIENT_CA_FILE")

	viper.BindEnv(GameTimeTicksPerSecond, "GAME_TIME_TICKS_PER_SECOND")
	viper.BindEnv(GameTimeDaySpeed, "GAME_TIME_DAY_SPEED")
	viper.BindEnv(PositionCheckpointSeconds, "AGENT_POSITION_CHECKPOINT_SECONDS")
}

func setDefaultValues() {
	viper.SetDefault(AgentHost, "127.0.0.1")
	viper.SetDefault(AgentPort, 15882)
	viper.SetDefault(AgentModuleId, "AgentServer")
	viper.SetDefault(AgentDataPath, "./Data")
	viper.SetDefault(AgentPrelinkedNavdataFile, "./prelinked_navdata.gob")
	viper.SetDefault(AgentLevelDataFile, "./Media/server_dep/silkroad/textdata/leveldata.txt")
	viper.SetDefault(AgentTrafficPacketLimit, 200)
	viper.SetDefault(AgentTrafficWindowMS, 1000)
	viper.SetDefault(AgentTimedBuffDuration, false)
	viper.SetDefault(AgentDiagnosticsEnabled, false)
	viper.SetDefault(AgentDiagnosticsPort, 6060)
	// A bare Agent defaults to loopback. Container deployments explicitly bind
	// this to their private service network through AGENT_ADMISSION_HOST.
	viper.SetDefault(AgentAdmissionHost, "127.0.0.1")
	viper.SetDefault(AgentAdmissionPort, 15883)

	viper.SetDefault(GameTimeTicksPerSecond, 10)
	viper.SetDefault(GameTimeDaySpeed, 1.0)
	viper.SetDefault(PositionCheckpointSeconds, 30)
}
