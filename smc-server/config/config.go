package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	frameworkconfig "github.com/Harvi8/AegisCore/framework/config"
	"github.com/spf13/viper"
)

const (
	Host                         = "smc.host"
	Port                         = "smc.port"
	AdminUsername                = "smc.admin.username"
	AdminPassword                = "smc.admin.password"
	AdminPasswordHash            = "smc.admin.password_hash"
	AllowWeakPassword            = "smc.admin.allow_weak_password"
	SessionTTL                   = "smc.session.ttl"
	CookieSecure                 = "smc.cookie.secure"
	AuditPath                    = "smc.audit.path"
	MediaExportRoot              = "smc.media_export.root"
	MediaExportBinary            = "smc.media_export.binary"
	MediaExportDisplayRoot       = "smc.media_export.display_root"
	GatewayAddress               = "smc.gateway.address"
	AgentAddress                 = "smc.agent.address"
	AgentManagementEnabled       = "smc.agent_management.enabled"
	AgentManagementEndpoint      = "smc.agent_management.endpoint"
	AgentManagementTLSCertFile   = "smc.agent_management.tls.cert_file"
	AgentManagementTLSKeyFile    = "smc.agent_management.tls.key_file"
	AgentManagementTLSCAFile     = "smc.agent_management.tls.ca_file"
	AgentManagementTLSServerName = "smc.agent_management.tls.server_name"
)

var ErrInvalidConfiguration = errors.New("invalid SMC Server configuration")

type Settings struct {
	Host                         string
	Port                         int
	AdminUsername                string
	AdminPassword                string
	AdminPasswordHash            string
	AllowWeakPassword            bool
	SessionTTL                   time.Duration
	CookieSecure                 bool
	AuditPath                    string
	MediaExportRoot              string
	MediaExportBinary            string
	MediaExportDisplayRoot       string
	GatewayAddress               string
	AgentAddress                 string
	AgentManagementEnabled       bool
	AgentManagementEndpoint      string
	AgentManagementTLSCertFile   string
	AgentManagementTLSKeyFile    string
	AgentManagementTLSCAFile     string
	AgentManagementTLSServerName string
}

func Initialize() {
	frameworkconfig.Initialize()

	viper.SetDefault(Host, "127.0.0.1")
	viper.SetDefault(Port, 15900)
	viper.SetDefault(AdminUsername, "admin")
	viper.SetDefault(AllowWeakPassword, false)
	viper.SetDefault(SessionTTL, "8h")
	viper.SetDefault(CookieSecure, false)
	viper.SetDefault(AuditPath, "smc-audit.jsonl")
	viper.SetDefault(MediaExportRoot, "media-exports")
	viper.SetDefault(MediaExportBinary, "sro-data")
	viper.SetDefault(MediaExportDisplayRoot, "media-exports")
	viper.SetDefault(GatewayAddress, "127.0.0.1:15779")
	viper.SetDefault(AgentAddress, "127.0.0.1:15882")
	viper.SetDefault(AgentManagementEnabled, false)
	viper.SetDefault(AgentManagementEndpoint, "https://127.0.0.1:15884")

	_ = viper.BindEnv(Host, "SMC_HOST")
	_ = viper.BindEnv(Port, "SMC_PORT")
	_ = viper.BindEnv(AdminUsername, "SMC_ADMIN_USERNAME")
	_ = viper.BindEnv(AdminPassword, "SMC_ADMIN_PASSWORD")
	_ = viper.BindEnv(AdminPasswordHash, "SMC_ADMIN_PASSWORD_HASH")
	_ = viper.BindEnv(AllowWeakPassword, "SMC_ALLOW_WEAK_PASSWORD")
	_ = viper.BindEnv(SessionTTL, "SMC_SESSION_TTL")
	_ = viper.BindEnv(CookieSecure, "SMC_COOKIE_SECURE")
	_ = viper.BindEnv(AuditPath, "SMC_AUDIT_PATH")
	_ = viper.BindEnv(MediaExportRoot, "SMC_MEDIA_EXPORT_ROOT")
	_ = viper.BindEnv(MediaExportBinary, "SMC_MEDIA_EXPORT_BINARY")
	_ = viper.BindEnv(MediaExportDisplayRoot, "SMC_MEDIA_EXPORT_DISPLAY_ROOT")
	_ = viper.BindEnv(GatewayAddress, "SMC_GATEWAY_ADDRESS")
	_ = viper.BindEnv(AgentAddress, "SMC_AGENT_ADDRESS")
	_ = viper.BindEnv(AgentManagementEnabled, "SMC_AGENT_MANAGEMENT_ENABLED")
	_ = viper.BindEnv(AgentManagementEndpoint, "SMC_AGENT_MANAGEMENT_ENDPOINT")
	_ = viper.BindEnv(AgentManagementTLSCertFile, "SMC_AGENT_MANAGEMENT_TLS_CERT_FILE")
	_ = viper.BindEnv(AgentManagementTLSKeyFile, "SMC_AGENT_MANAGEMENT_TLS_KEY_FILE")
	_ = viper.BindEnv(AgentManagementTLSCAFile, "SMC_AGENT_MANAGEMENT_TLS_CA_FILE")
	_ = viper.BindEnv(AgentManagementTLSServerName, "SMC_AGENT_MANAGEMENT_TLS_SERVER_NAME")
}

func Load() (Settings, error) {
	ttl, err := time.ParseDuration(strings.TrimSpace(viper.GetString(SessionTTL)))
	if err != nil || ttl < 5*time.Minute || ttl > 24*time.Hour {
		return Settings{}, fmt.Errorf("%w: SMC_SESSION_TTL must be between 5m and 24h", ErrInvalidConfiguration)
	}
	settings := Settings{
		Host:                         strings.TrimSpace(viper.GetString(Host)),
		Port:                         viper.GetInt(Port),
		AdminUsername:                strings.TrimSpace(viper.GetString(AdminUsername)),
		AdminPassword:                viper.GetString(AdminPassword),
		AdminPasswordHash:            strings.TrimSpace(viper.GetString(AdminPasswordHash)),
		AllowWeakPassword:            viper.GetBool(AllowWeakPassword),
		SessionTTL:                   ttl,
		CookieSecure:                 viper.GetBool(CookieSecure),
		AuditPath:                    strings.TrimSpace(viper.GetString(AuditPath)),
		MediaExportRoot:              strings.TrimSpace(viper.GetString(MediaExportRoot)),
		MediaExportBinary:            strings.TrimSpace(viper.GetString(MediaExportBinary)),
		GatewayAddress:               strings.TrimSpace(viper.GetString(GatewayAddress)),
		MediaExportDisplayRoot:       strings.TrimSpace(viper.GetString(MediaExportDisplayRoot)),
		AgentAddress:                 strings.TrimSpace(viper.GetString(AgentAddress)),
		AgentManagementEnabled:       viper.GetBool(AgentManagementEnabled),
		AgentManagementEndpoint:      strings.TrimSpace(viper.GetString(AgentManagementEndpoint)),
		AgentManagementTLSCertFile:   strings.TrimSpace(viper.GetString(AgentManagementTLSCertFile)),
		AgentManagementTLSKeyFile:    strings.TrimSpace(viper.GetString(AgentManagementTLSKeyFile)),
		AgentManagementTLSCAFile:     strings.TrimSpace(viper.GetString(AgentManagementTLSCAFile)),
		AgentManagementTLSServerName: strings.TrimSpace(viper.GetString(AgentManagementTLSServerName)),
	}
	if settings.Host == "" || settings.Port < 1 || settings.Port > 65535 {
		return Settings{}, fmt.Errorf("%w: SMC host and a valid port are required", ErrInvalidConfiguration)
	}
	if settings.AdminUsername == "" || len(settings.AdminUsername) > 64 {
		return Settings{}, fmt.Errorf("%w: SMC_ADMIN_USERNAME must be 1..64 characters", ErrInvalidConfiguration)
	}
	if (settings.AdminPassword == "") == (settings.AdminPasswordHash == "") {
		return Settings{}, fmt.Errorf("%w: set exactly one of SMC_ADMIN_PASSWORD or SMC_ADMIN_PASSWORD_HASH", ErrInvalidConfiguration)
	}
	if settings.AdminPassword != "" && len(settings.AdminPassword) < 12 && !settings.AllowWeakPassword {
		return Settings{}, fmt.Errorf("%w: SMC_ADMIN_PASSWORD must contain at least 12 characters unless SMC_ALLOW_WEAK_PASSWORD is enabled", ErrInvalidConfiguration)
	}
	if settings.AuditPath == "" {
		return Settings{}, fmt.Errorf("%w: SMC_AUDIT_PATH is required", ErrInvalidConfiguration)
	}
	if settings.MediaExportRoot == "" || settings.MediaExportDisplayRoot == "" || settings.MediaExportBinary == "" {
		return Settings{}, fmt.Errorf("%w: SMC Media export root, display root, and binary are required", ErrInvalidConfiguration)
	}
	for name, address := range map[string]string{"SMC_GATEWAY_ADDRESS": settings.GatewayAddress, "SMC_AGENT_ADDRESS": settings.AgentAddress} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return Settings{}, fmt.Errorf("%w: %s must be a host:port address", ErrInvalidConfiguration, name)
		}
	}
	if settings.AgentManagementEnabled {
		if !strings.HasPrefix(settings.AgentManagementEndpoint, "https://") {
			return Settings{}, fmt.Errorf("%w: SMC_AGENT_MANAGEMENT_ENDPOINT must use https", ErrInvalidConfiguration)
		}
		if settings.AgentManagementTLSCertFile == "" || settings.AgentManagementTLSKeyFile == "" || settings.AgentManagementTLSCAFile == "" || settings.AgentManagementTLSServerName == "" {
			return Settings{}, fmt.Errorf("%w: complete SMC Agent management mTLS settings are required when enabled", ErrInvalidConfiguration)
		}
	}
	return settings, nil
}
