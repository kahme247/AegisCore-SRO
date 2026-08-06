package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestDefaultsProduceExpectedDatabaseConfiguration(t *testing.T) {
	resetViper(t)
	setDefaultValues()

	assertConfigValue(t, DatabaseAccountDriver, "mysql")
	assertConfigValue(t, DatabaseAccountHost, "127.0.0.1")
	assertConfigValue(t, DatabaseAccountPort, 3306)
	assertConfigValue(t, DatabaseAccountUser, "sro")
	assertConfigValue(t, DatabaseAccountPassword, nil)
	assertConfigValue(t, DatabaseAccountDatabase, "SRO_ACCOUNT")
	assertConfigValue(t, DatabaseAccountParams, "?parseTime=true")
	assertConfigValue(t, DatabaseShardDatabase, "SRO_SHARD")
	assertConfigValue(t, LogLevel, "info")

	if got, want := ConnStringAccount(), "sro:@tcp(127.0.0.1:3306)/SRO_ACCOUNT?parseTime=true"; got != want {
		t.Fatalf("ConnStringAccount() = %q, want %q", got, want)
	}
	if got, want := ConnStringShard(), "sro:@tcp(127.0.0.1:3306)/SRO_SHARD?parseTime=true"; got != want {
		t.Fatalf("ConnStringShard() = %q, want %q", got, want)
	}
}

func TestEnvironmentBindingsOverrideDefaultsAndConvertScalars(t *testing.T) {
	resetViper(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("DATABASE_ACCOUNT_DRIVER", "mysql")
	t.Setenv("DB_ACCOUNT_HOST", "account-db")
	t.Setenv("DB_ACCOUNT_PORT", "3307")
	t.Setenv("DB_ACCOUNT_USER", "account-user")
	t.Setenv("DB_ACCOUNT_PW", "account-password")
	t.Setenv("DB_ACCOUNT_DATABASE", "ACCOUNT_TEST")
	t.Setenv("DB_ACCOUNT_PARAMS", "?parseTime=true&timeout=2s")

	if got := viper.GetInt(DatabaseAccountPort); got != 3307 {
		t.Fatalf("account port = %d, want 3307", got)
	}
	if got, want := ConnStringAccount(), "account-user:account-password@tcp(account-db:3307)/ACCOUNT_TEST?parseTime=true&timeout=2s"; got != want {
		t.Fatalf("ConnStringAccount() = %q, want %q", got, want)
	}
}

func TestEmptyEnvironmentValuesFallBackToDefaults(t *testing.T) {
	resetViper(t)
	setDefaultValues()
	bindEnvAliases()

	t.Setenv("DB_ACCOUNT_PW", "")
	t.Setenv("DB_ACCOUNT_PARAMS", "")

	assertConfigValue(t, DatabaseAccountPassword, nil)
	assertConfigValue(t, DatabaseAccountParams, "?parseTime=true")
}

func TestEnvironmentOverridesConfigFileAndConfigOverridesDefault(t *testing.T) {
	resetViper(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("db:\n  account:\n    host: file-db\n    port: 3308\n    user: file-user\n")
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	setDefaultValues()
	bindEnvAliases()
	viper.SetConfigFile(configPath)
	t.Setenv("DB_ACCOUNT_HOST", "env-db")
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	assertConfigValue(t, DatabaseAccountHost, "env-db")
	assertConfigValue(t, DatabaseAccountPort, 3308)
	assertConfigValue(t, DatabaseAccountUser, "file-user")
	if got := viper.ConfigFileUsed(); got != configPath {
		t.Fatalf("ConfigFileUsed() = %q, want %q", got, configPath)
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
