package config

import (
	"errors"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadRejectsWeakPasswordByDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("SMC_ADMIN_PASSWORD", "1")
	t.Setenv("SMC_ALLOW_WEAK_PASSWORD", "0")
	Initialize()
	_, err := Load()
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Load error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestLoadAllowsExplicitLocalWeakPassword(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("SMC_ADMIN_USERNAME", "1")
	t.Setenv("SMC_ADMIN_PASSWORD", "1")
	t.Setenv("SMC_ALLOW_WEAK_PASSWORD", "1")
	t.Setenv("SMC_AGENT_MANAGEMENT_ENABLED", "0")
	Initialize()
	settings, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.AdminUsername != "1" || settings.AdminPassword != "1" || !settings.AllowWeakPassword {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadRejectsEnabledAgentManagementWithoutMTLS(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("SMC_ADMIN_PASSWORD", "correct horse battery staple")
	t.Setenv("SMC_AGENT_MANAGEMENT_ENABLED", "1")
	Initialize()
	_, err := Load()
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Load error = %v, want ErrInvalidConfiguration", err)
	}
}
