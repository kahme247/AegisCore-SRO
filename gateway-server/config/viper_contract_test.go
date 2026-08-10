package config

import (
	"net/netip"
	"testing"

	"github.com/spf13/viper"
)

func TestGatewayDefaults(t *testing.T) {
	resetViperContract(t)
	setDefaultValues()

	assertGatewayConfigValue(t, AgentHost, "127.0.0.1")
	assertGatewayConfigValue(t, AgentPublicIp, "127.0.0.1")
	assertGatewayConfigValue(t, AgentPort, 15882)
	assertGatewayConfigValue(t, AgentAdmissionPort, 15883)
	assertGatewayConfigValue(t, AgentAdmissionTimeoutSeconds, 5)
	assertGatewayConfigValue(t, GatewayHost, "127.0.0.1")
	assertGatewayConfigValue(t, GatewayPort, 15779)
	assertGatewayConfigValue(t, GatewayEnvironment, EnvironmentProduction)
	assertGatewayConfigValue(t, GatewayProtocolProfileName, VSRO188Profile)
	assertGatewayConfigValue(t, GatewayFarmID, 1)
	assertGatewayConfigValue(t, GatewayFarmName, "AegisCore")

	for _, key := range []string{GatewayProtocolContentID, GatewayProtocolModuleName, GatewayProtocolVersion, AgentAdmissionTLSCAFile, AgentAdmissionTLSServerName, GatewayAdmissionTLSCertFile, GatewayAdmissionTLSKeyFile} {
		if viper.IsSet(key) {
			t.Fatalf("%s must not have a default", key)
		}
	}
}

func TestGatewayEnvironmentBindingsLoadValidatedRuntime(t *testing.T) {
	resetViperContract(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("GATEWAY_PROTOCOL_CONTENT_ID", "22")
	t.Setenv("GATEWAY_PROTOCOL_MODULE_NAME", "SR_Client")
	t.Setenv("GATEWAY_PROTOCOL_VERSION", "188")
	t.Setenv("GATEWAY_FARM_ID", "2")
	t.Setenv("GATEWAY_FARM_NAME", "Test Farm")
	t.Setenv("GATEWAY_ENVIRONMENT", "development")
	t.Setenv("AGENT_PUBLIC_IP", "127.0.0.1")
	t.Setenv("AGENT_PORT", "15882")

	profile, err := LoadProtocolProfile()
	if err != nil {
		t.Fatalf("LoadProtocolProfile: %v", err)
	}
	if profile != (ProtocolProfile{Name: VSRO188Profile, ContentID: 22, ModuleName: "SR_Client", Version: 188}) {
		t.Fatalf("profile = %#v", profile)
	}
	farm, err := LoadFarm()
	if err != nil {
		t.Fatalf("LoadFarm: %v", err)
	}
	if farm != (Farm{ID: 2, Name: "Test Farm"}) {
		t.Fatalf("farm = %#v", farm)
	}
	endpoint, err := LoadAgentEndpoint()
	if err != nil {
		t.Fatalf("LoadAgentEndpoint: %v", err)
	}
	if endpoint != (AgentEndpoint{IP: netip.MustParseAddr("127.0.0.1"), Port: 15882}) {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	if err := ValidateGatewayRuntime(); err != nil {
		t.Fatalf("ValidateGatewayRuntime: %v", err)
	}
}

func TestGatewayEnvironmentBindingsLoadVSRO274Runtime(t *testing.T) {
	resetViperContract(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("GATEWAY_PROTOCOL_PROFILE", VSRO274Profile)
	t.Setenv("GATEWAY_PROTOCOL_CONTENT_ID", "22")
	t.Setenv("GATEWAY_PROTOCOL_MODULE_NAME", "SR_Client")
	t.Setenv("GATEWAY_PROTOCOL_VERSION", "274")

	profile, err := LoadProtocolProfile()
	if err != nil {
		t.Fatalf("LoadProtocolProfile: %v", err)
	}
	if profile != (ProtocolProfile{Name: VSRO274Profile, ContentID: 22, ModuleName: "SR_Client", Version: 274}) {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestGatewayEnvironmentBindingsLoadMultipleProfiles(t *testing.T) {
	resetViperContract(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("GATEWAY_PROTOCOL_PROFILES", "vsro188:22:SR_Client:188,vsro274:22:SR_Client:274")
	profiles, err := LoadProtocolProfiles()
	if err != nil {
		t.Fatalf("LoadProtocolProfiles: %v", err)
	}
	want := []ProtocolProfile{
		{Name: VSRO188Profile, ContentID: 22, ModuleName: "SR_Client", Version: 188},
		{Name: VSRO274Profile, ContentID: 22, ModuleName: "SR_Client", Version: 274},
	}
	if len(profiles) != len(want) {
		t.Fatalf("profiles = %#v, want %#v", profiles, want)
	}
	for index := range want {
		if profiles[index] != want[index] {
			t.Fatalf("profiles[%d] = %#v, want %#v", index, profiles[index], want[index])
		}
	}
}

func TestGatewayMalformedEnvironmentScalarsFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		envName  string
		envValue string
		load     func() error
	}{
		{name: "content ID", envName: "GATEWAY_PROTOCOL_CONTENT_ID", envValue: "not-a-number", load: func() error { _, err := LoadProtocolProfile(); return err }},
		{name: "farm ID", envName: "GATEWAY_FARM_ID", envValue: "256", load: func() error { _, err := LoadFarm(); return err }},
		{name: "agent port", envName: "AGENT_PORT", envValue: "65536", load: func() error { _, err := LoadAgentEndpoint(); return err }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetViperContract(t)
			setDefaultValues()
			bindEnvAliases()
			t.Setenv("GATEWAY_PROTOCOL_CONTENT_ID", "22")
			t.Setenv("GATEWAY_PROTOCOL_MODULE_NAME", "SR_Client")
			t.Setenv("GATEWAY_PROTOCOL_VERSION", "188")
			t.Setenv("GATEWAY_ENVIRONMENT", "development")
			t.Setenv("AGENT_PUBLIC_IP", "127.0.0.1")
			t.Setenv(test.envName, test.envValue)

			if err := test.load(); err == nil {
				t.Fatal("expected invalid environment value to be rejected")
			}
		})
	}
}

func resetViperContract(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
}

func assertGatewayConfigValue(t *testing.T, key string, want any) {
	t.Helper()
	if got := viper.Get(key); got != want {
		t.Fatalf("viper.Get(%q) = %#v, want %#v", key, got, want)
	}
}
