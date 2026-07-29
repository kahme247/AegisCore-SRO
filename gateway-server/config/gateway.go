package config

import (
	"fmt"
	"math"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/ferdoran/go-sro/framework/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	AgentHost     = "agent.host"
	AgentPublicIp = "agent.public_ip"
	AgentPort     = "agent.port"
	AgentModuleId = "agent.module_id"

	// AgentAdmission* configures Gateway's private mTLS installation client.
	// Certificate paths and ServerName intentionally have no defaults: startup
	// must fail rather than fall back to a shared backend password.
	AgentAdmissionPort           = "agent.admission.port"
	AgentAdmissionTLSCAFile      = "agent.admission.tls.ca_file"
	AgentAdmissionTLSServerName  = "agent.admission.tls.server_name"
	GatewayAdmissionTLSCertFile  = "gateway.admission.tls.cert_file"
	GatewayAdmissionTLSKeyFile   = "gateway.admission.tls.key_file"
	AgentAdmissionTimeoutSeconds = "agent.admission.timeout_seconds"

	// LoginAttempt* bounds pre-credential account/IP abuse tracking. Values are
	// explicit so operators can tune the window without an unbounded map.
	LoginAttemptWindowSeconds   = "gateway.login_attempt.window_seconds"
	LoginAccountAttemptLimit    = "gateway.login_attempt.account_limit"
	LoginIPAttemptLimit         = "gateway.login_attempt.ip_limit"
	LoginAttemptTrackerCapacity = "gateway.login_attempt.capacity"

	GatewayHost               = "gateway.ip"
	GatewayPort               = "gateway.Port"
	GatewayModuleId           = "gateway.module_id"
	GatewayTrafficPacketLimit = "gateway.traffic_filter.packet_limit"
	GatewayTrafficWindowMS    = "gateway.traffic_filter.window_ms"

	// GatewayEnvironment is deliberately production by default. Local
	// loopback Agent advertisements require an explicit development setting so
	// a production deployment cannot accidentally send clients 127.0.0.1.
	GatewayEnvironment = "gateway.environment"

	// GatewayProtocol* pins the one client profile that Gateway will accept for
	// patch and notice negotiation. The evidence-pending profile deliberately
	// has no ContentID/module/version defaults; operators must set all three
	// from an approved vSRO 1.88 capture before Gateway admits those requests.
	GatewayProtocolProfileName = "gateway.protocol.profile"
	GatewayProtocolContentID   = "gateway.protocol.content_id"
	GatewayProtocolModuleName  = "gateway.protocol.module_name"
	GatewayProtocolVersion     = "gateway.protocol.version"

	// GatewayFarm* controls the documented farm section at the start of the
	// shard-list response. Keeping it configurable avoids silently advertising
	// a historical hard-coded farm identity.
	GatewayFarmID   = "gateway.farm.id"
	GatewayFarmName = "gateway.farm.name"
)

const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
	VSRO188Profile         = "vsro188"
)

// ProtocolProfile is the capture-backed Gateway contract for the single
// supported client. It intentionally contains only values documented in the
// 0x6100 patch request.
type ProtocolProfile struct {
	Name       string
	ContentID  byte
	ModuleName string
	Version    uint32
}

// Farm is the first loop in the documented 0xA101 shard-list response.
type Farm struct {
	ID   byte
	Name string
}

// AgentEndpoint is the IPv4 address Gateway advertises in 0xA106 (and which
// the login path can validate before it publishes an Agent endpoint).
type AgentEndpoint struct {
	IP   netip.Addr
	Port uint16
}

func Initialize() {
	config.Initialize()

	setDefaultValues()
	bindEnvAliases()

	logrus.Info("gateway config initialized")
}

func bindEnvAliases() {
	viper.BindEnv(AgentHost, "AGENT_HOST")
	viper.BindEnv(AgentPublicIp, "AGENT_PUBLIC_IP")
	viper.BindEnv(AgentPort, "AGENT_PORT")
	viper.BindEnv(AgentModuleId, "AGENT_MODULE_ID")
	viper.BindEnv(AgentAdmissionPort, "AGENT_ADMISSION_PORT")
	viper.BindEnv(AgentAdmissionTLSCAFile, "AGENT_ADMISSION_TLS_CA_FILE")
	viper.BindEnv(AgentAdmissionTLSServerName, "AGENT_ADMISSION_TLS_SERVER_NAME")
	viper.BindEnv(GatewayAdmissionTLSCertFile, "GATEWAY_ADMISSION_TLS_CERT_FILE")
	viper.BindEnv(GatewayAdmissionTLSKeyFile, "GATEWAY_ADMISSION_TLS_KEY_FILE")
	viper.BindEnv(AgentAdmissionTimeoutSeconds, "AGENT_ADMISSION_TIMEOUT_SECONDS")
	viper.BindEnv(LoginAttemptWindowSeconds, "GATEWAY_LOGIN_ATTEMPT_WINDOW_SECONDS")
	viper.BindEnv(LoginAccountAttemptLimit, "GATEWAY_LOGIN_ACCOUNT_ATTEMPT_LIMIT")
	viper.BindEnv(LoginIPAttemptLimit, "GATEWAY_LOGIN_IP_ATTEMPT_LIMIT")
	viper.BindEnv(LoginAttemptTrackerCapacity, "GATEWAY_LOGIN_ATTEMPT_TRACKER_CAPACITY")

	viper.BindEnv(GatewayHost, "GATEWAY_HOST")
	viper.BindEnv(GatewayPort, "GATEWAY_PORT")
	viper.BindEnv(GatewayModuleId, "GATEWAY_MODULE_ID")
	viper.BindEnv(GatewayTrafficPacketLimit, "GATEWAY_TRAFFIC_FILTER_PACKET_LIMIT")
	viper.BindEnv(GatewayTrafficWindowMS, "GATEWAY_TRAFFIC_FILTER_WINDOW_MS")
	viper.BindEnv(GatewayEnvironment, "GATEWAY_ENVIRONMENT")
	viper.BindEnv(GatewayProtocolProfileName, "GATEWAY_PROTOCOL_PROFILE")
	viper.BindEnv(GatewayProtocolContentID, "GATEWAY_PROTOCOL_CONTENT_ID")
	viper.BindEnv(GatewayProtocolModuleName, "GATEWAY_PROTOCOL_MODULE_NAME")
	viper.BindEnv(GatewayProtocolVersion, "GATEWAY_PROTOCOL_VERSION")
	viper.BindEnv(GatewayFarmID, "GATEWAY_FARM_ID")
	viper.BindEnv(GatewayFarmName, "GATEWAY_FARM_NAME")
}

func setDefaultValues() {
	viper.SetDefault(AgentHost, "127.0.0.1")
	viper.SetDefault(AgentPublicIp, "127.0.0.1")
	viper.SetDefault(AgentPort, 15882)
	viper.SetDefault(AgentModuleId, "AgentServer")
	viper.SetDefault(AgentAdmissionPort, 15883)
	viper.SetDefault(AgentAdmissionTimeoutSeconds, 5)
	viper.SetDefault(LoginAttemptWindowSeconds, 15*60)
	viper.SetDefault(LoginAccountAttemptLimit, 4)
	viper.SetDefault(LoginIPAttemptLimit, 20)
	viper.SetDefault(LoginAttemptTrackerCapacity, 4096)

	viper.SetDefault(GatewayHost, "127.0.0.1")
	viper.SetDefault(GatewayPort, 15779)
	viper.SetDefault(GatewayModuleId, "GatewayServer")
	viper.SetDefault(GatewayTrafficPacketLimit, 200)
	viper.SetDefault(GatewayTrafficWindowMS, 1000)
	viper.SetDefault(GatewayEnvironment, EnvironmentProduction)
	viper.SetDefault(GatewayProtocolProfileName, VSRO188Profile)
	viper.SetDefault(GatewayFarmID, 1)
	viper.SetDefault(GatewayFarmName, "SRO_DEUTSCHLAND")
}

// LoadProtocolProfile returns the exact single-client Gateway contract. There
// are no fallback ContentID/module/version guesses: protocol/profiles/vsro188
// is evidence-pending, and accepting guessed values would create a false
// compatibility claim.
func LoadProtocolProfile() (ProtocolProfile, error) {
	contentID := viper.GetInt64(GatewayProtocolContentID)
	version := viper.GetInt64(GatewayProtocolVersion)
	profile := ProtocolProfile{
		Name:       viper.GetString(GatewayProtocolProfileName),
		ModuleName: viper.GetString(GatewayProtocolModuleName),
	}
	if contentID > 0 && contentID <= math.MaxUint8 {
		profile.ContentID = byte(contentID)
	}
	if version > 0 && version <= math.MaxUint32 {
		profile.Version = uint32(version)
	}
	if err := ValidateProtocolProfile(profile); err != nil {
		return ProtocolProfile{}, err
	}
	return profile, nil
}

// ValidateProtocolProfile performs the pure validation shared by startup and
// request handlers. Keeping it independent from Viper makes the capture-bound
// contract straightforward to test.
func ValidateProtocolProfile(profile ProtocolProfile) error {
	if profile.Name != VSRO188Profile {
		return fmt.Errorf("unsupported gateway protocol profile %q", profile.Name)
	}
	if profile.ContentID == 0 {
		return fmt.Errorf("gateway protocol content ID must be configured from an approved capture")
	}
	if profile.ModuleName == "" || strings.TrimSpace(profile.ModuleName) != profile.ModuleName || strings.IndexByte(profile.ModuleName, 0) >= 0 {
		return fmt.Errorf("gateway protocol module name must be a non-blank, NUL-free exact value")
	}
	if !utf8.ValidString(profile.ModuleName) || len(profile.ModuleName) > math.MaxUint16 {
		return fmt.Errorf("gateway protocol module name is not serializable")
	}
	if profile.Version == 0 {
		return fmt.Errorf("gateway protocol version must be configured from an approved capture")
	}
	return nil
}

// LoadFarm returns the farm record Gateway writes before the shard loop.
func LoadFarm() (Farm, error) {
	id := viper.GetInt64(GatewayFarmID)
	farm := Farm{Name: viper.GetString(GatewayFarmName)}
	if id > 0 && id <= math.MaxUint8 {
		farm.ID = byte(id)
	}
	if err := ValidateFarm(farm); err != nil {
		return Farm{}, err
	}
	return farm, nil
}

func ValidateFarm(farm Farm) error {
	if farm.ID == 0 {
		return fmt.Errorf("gateway farm ID must be 1..255")
	}
	if farm.Name == "" || strings.TrimSpace(farm.Name) != farm.Name || strings.IndexByte(farm.Name, 0) >= 0 {
		return fmt.Errorf("gateway farm name must be a non-blank, NUL-free value")
	}
	if !utf8.ValidString(farm.Name) || len(farm.Name) > math.MaxUint16 {
		return fmt.Errorf("gateway farm name is not serializable")
	}
	return nil
}

// LoadAgentEndpoint validates the advertised Agent address before Gateway
// writes it into a client packet. This protects both 0xA106 and callers that
// use the same endpoint for successful 0xA102 responses.
func LoadAgentEndpoint() (AgentEndpoint, error) {
	ip, err := netip.ParseAddr(viper.GetString(AgentPublicIp))
	if err != nil {
		return AgentEndpoint{}, fmt.Errorf("parse agent public IP: %w", err)
	}
	endpoint := AgentEndpoint{IP: ip}
	port := viper.GetInt64(AgentPort)
	if port > 0 && port <= math.MaxUint16 {
		endpoint.Port = uint16(port)
	}
	if err := ValidateAgentEndpoint(viper.GetString(GatewayEnvironment), endpoint); err != nil {
		return AgentEndpoint{}, err
	}
	return endpoint, nil
}

// ValidateAgentEndpoint only permits an externally routable IPv4 address in
// production. Development is explicit and still rejects unspecified,
// multicast, and non-IPv4 values because the wire field is exactly four
// IPv4 bytes.
func ValidateAgentEndpoint(environment string, endpoint AgentEndpoint) error {
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment != EnvironmentDevelopment && environment != EnvironmentProduction {
		return fmt.Errorf("gateway environment must be %q or %q", EnvironmentDevelopment, EnvironmentProduction)
	}
	if !endpoint.IP.IsValid() || !endpoint.IP.Is4() {
		return fmt.Errorf("agent public IP must be a valid IPv4 address")
	}
	if endpoint.IP.IsUnspecified() || endpoint.IP.IsMulticast() || endpoint.IP == netip.MustParseAddr("255.255.255.255") {
		return fmt.Errorf("agent public IP must be a unicast address")
	}
	if endpoint.Port == 0 {
		return fmt.Errorf("agent public port must be 1..65535")
	}
	if environment == EnvironmentProduction {
		if endpoint.IP.IsLoopback() || endpoint.IP.IsPrivate() || endpoint.IP.IsLinkLocalUnicast() || !endpoint.IP.IsGlobalUnicast() {
			return fmt.Errorf("production Agent endpoint must use an externally routable IPv4 address")
		}
	}
	return nil
}

// ValidateGatewayRuntime gives the executable a single fail-closed startup
// check. Handlers call the narrower loaders as defense in depth so a later
// configuration reload cannot reintroduce an unsafe advertised endpoint.
func ValidateGatewayRuntime() error {
	if _, err := LoadProtocolProfile(); err != nil {
		return err
	}
	if _, err := LoadFarm(); err != nil {
		return err
	}
	if _, err := LoadAgentEndpoint(); err != nil {
		return err
	}
	return nil
}
