package config

import (
	"math"
	"net/netip"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestValidateProtocolProfileRequiresCaptureBackedValues(t *testing.T) {
	for _, valid := range []ProtocolProfile{
		{Name: VSRO188Profile, ContentID: 122, ModuleName: "SR_Client", Version: 188},
		{Name: VSRO274Profile, ContentID: 22, ModuleName: "SR_Client", Version: 274},
	} {
		if err := ValidateProtocolProfile(valid); err != nil {
			t.Fatalf("ValidateProtocolProfile(%+v) error = %v", valid, err)
		}
	}

	for _, profile := range []ProtocolProfile{
		{Name: VSRO188Profile, ModuleName: "SR_Client", Version: 188},
		{Name: VSRO188Profile, ContentID: 122, Version: 188},
		{Name: VSRO188Profile, ContentID: 122, ModuleName: "SR_Client"},
		{Name: "other", ContentID: 122, ModuleName: "SR_Client", Version: 188},
	} {
		if err := ValidateProtocolProfile(profile); err == nil {
			t.Fatalf("ValidateProtocolProfile(%+v) error = nil, want rejection", profile)
		}
	}
}

func TestValidateAgentEndpointRequiresExternalIPv4InProduction(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		ip          string
		port        uint16
		wantErr     bool
	}{
		{name: "development loopback", environment: EnvironmentDevelopment, ip: "127.0.0.1", port: 15882},
		{name: "production public", environment: EnvironmentProduction, ip: "203.0.113.42", port: 15882},
		{name: "production loopback", environment: EnvironmentProduction, ip: "127.0.0.1", port: 15882, wantErr: true},
		{name: "production private", environment: EnvironmentProduction, ip: "10.0.0.2", port: 15882, wantErr: true},
		{name: "IPv6", environment: EnvironmentDevelopment, ip: "2001:db8::1", port: 15882, wantErr: true},
		{name: "zero port", environment: EnvironmentProduction, ip: "203.0.113.42", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateAgentEndpoint(test.environment, AgentEndpoint{IP: netip.MustParseAddr(test.ip), Port: test.port})
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateAgentEndpoint() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestValidateFarmRejectsEmptyOrZeroFields(t *testing.T) {
	if err := ValidateFarm(Farm{ID: 1, Name: "AegisCore"}); err != nil {
		t.Fatalf("ValidateFarm(valid) error = %v", err)
	}
	for _, farm := range []Farm{{Name: "Farm"}, {ID: 1}, {ID: 1, Name: " Farm"}} {
		if err := ValidateFarm(farm); err == nil {
			t.Fatalf("ValidateFarm(%+v) error = nil, want rejection", farm)
		}
	}
}

func TestGatewayWireTextValidationPreservesCallerErrors(t *testing.T) {
	tooLong := strings.Repeat("a", math.MaxUint16+1)
	tests := []struct {
		name         string
		value        string
		profileError string
		farmError    string
	}{
		{
			name:         "blank",
			profileError: "gateway protocol module name must be a non-blank, NUL-free exact value",
			farmError:    "gateway farm name must be a non-blank, NUL-free value",
		},
		{
			name:         "whitespace",
			value:        " module",
			profileError: "gateway protocol module name must be a non-blank, NUL-free exact value",
			farmError:    "gateway farm name must be a non-blank, NUL-free value",
		},
		{
			name:         "NUL",
			value:        "module\x00name",
			profileError: "gateway protocol module name must be a non-blank, NUL-free exact value",
			farmError:    "gateway farm name must be a non-blank, NUL-free value",
		},
		{
			name:         "invalid UTF-8",
			value:        string([]byte{0xff}),
			profileError: "gateway protocol module name is not serializable",
			farmError:    "gateway farm name is not serializable",
		},
		{
			name:         "too long",
			value:        tooLong,
			profileError: "gateway protocol module name is not serializable",
			farmError:    "gateway farm name is not serializable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := ProtocolProfile{Name: VSRO188Profile, ContentID: 122, ModuleName: test.value, Version: 188}
			if err := ValidateProtocolProfile(profile); err == nil || err.Error() != test.profileError {
				t.Errorf("ValidateProtocolProfile() error = %v, want %q", err, test.profileError)
			}

			if err := ValidateFarm(Farm{ID: 1, Name: test.value}); err == nil || err.Error() != test.farmError {
				t.Errorf("ValidateFarm() error = %v, want %q", err, test.farmError)
			}
		})
	}
}

func TestValidateGatewayRuntimeFailsClosedWithoutCaptureBackedProfile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set(GatewayProtocolProfileName, VSRO188Profile)
	viper.Set(GatewayProtocolContentID, 122)
	viper.Set(GatewayProtocolModuleName, "SR_Client")
	viper.Set(GatewayProtocolVersion, 188)
	viper.Set(GatewayFarmID, 1)
	viper.Set(GatewayFarmName, "Test Farm")
	viper.Set(GatewayEnvironment, EnvironmentProduction)
	viper.Set(AgentPublicIp, "8.8.8.8")
	viper.Set(AgentPort, 15882)

	if err := ValidateGatewayRuntime(); err != nil {
		t.Fatalf("ValidateGatewayRuntime() error = %v", err)
	}

	viper.Set(GatewayProtocolVersion, 0)
	if err := ValidateGatewayRuntime(); err == nil {
		t.Fatal("ValidateGatewayRuntime() error = nil without a capture-backed version")
	}
}
