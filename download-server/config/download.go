package config

import (
	"errors"
	"path/filepath"

	frameworkconfig "github.com/Harvi8/AegisCore/framework/config"
	"github.com/spf13/viper"
)

const (
	Host               = "download.ip"
	Port               = "download.port"
	ModuleID           = "download.module_id"
	ManifestPath       = "download.manifest_path"
	StorageRoot        = "download.storage_root"
	ChunkSize          = "download.chunk_size"
	TrafficPacketLimit = "download.traffic_filter.packet_limit"
	TrafficWindowMS    = "download.traffic_filter.window_ms"
	SessionIdleSeconds = "download.session_idle_seconds"
	MaxTransfers       = "download.max_transfers"
	MaxTransfersPerIP  = "download.max_transfers_per_ip"
	HealthHost         = "download.health.host"
	HealthPort         = "download.health.port"
)

func Initialize() {
	frameworkconfig.Initialize()
	viper.SetDefault(Host, "0.0.0.0")
	viper.SetDefault(Port, 15880)
	viper.SetDefault(ModuleID, "DownloadServer")
	viper.SetDefault(ManifestPath, "patches/manifest.json")
	viper.SetDefault(StorageRoot, "patches/files")
	viper.SetDefault(ChunkSize, 16*1024)
	viper.SetDefault(TrafficPacketLimit, 50)
	viper.SetDefault(TrafficWindowMS, 1000)
	viper.SetDefault(SessionIdleSeconds, 30*60)
	viper.SetDefault(MaxTransfers, 32)
	viper.SetDefault(MaxTransfersPerIP, 4)
	viper.SetDefault(HealthHost, "0.0.0.0")
	viper.SetDefault(HealthPort, 15881)

	_ = viper.BindEnv(Host, "DOWNLOAD_HOST")
	_ = viper.BindEnv(Port, "DOWNLOAD_PORT")
	_ = viper.BindEnv(ModuleID, "DOWNLOAD_MODULE_ID")
	_ = viper.BindEnv(ManifestPath, "DOWNLOAD_MANIFEST_PATH")
	_ = viper.BindEnv(StorageRoot, "DOWNLOAD_STORAGE_ROOT")
	_ = viper.BindEnv(ChunkSize, "DOWNLOAD_CHUNK_SIZE")
	_ = viper.BindEnv(TrafficPacketLimit, "DOWNLOAD_TRAFFIC_PACKET_LIMIT")
	_ = viper.BindEnv(TrafficWindowMS, "DOWNLOAD_TRAFFIC_WINDOW_MS")
	_ = viper.BindEnv(SessionIdleSeconds, "DOWNLOAD_SESSION_IDLE_SECONDS")
	_ = viper.BindEnv(MaxTransfers, "DOWNLOAD_MAX_TRANSFERS")
	_ = viper.BindEnv(MaxTransfersPerIP, "DOWNLOAD_MAX_TRANSFERS_PER_IP")
	_ = viper.BindEnv(HealthHost, "DOWNLOAD_HEALTH_HOST")
	_ = viper.BindEnv(HealthPort, "DOWNLOAD_HEALTH_PORT")
}

func Validate() error {
	if viper.GetInt(Port) < 1 || viper.GetInt(Port) > 65535 {
		return errors.New("download port must be 1..65535")
	}
	if viper.GetString(ModuleID) == "" {
		return errors.New("download module ID must not be blank")
	}
	if viper.GetInt(ChunkSize) < 1 || viper.GetInt(ChunkSize) > 0x7FFF {
		return errors.New("download chunk size must be 1..32767")
	}
	if viper.GetInt(SessionIdleSeconds) < 1 {
		return errors.New("download session idle timeout must be positive")
	}
	if viper.GetInt(MaxTransfers) < 1 || viper.GetInt(MaxTransfersPerIP) < 1 || viper.GetInt(MaxTransfersPerIP) > viper.GetInt(MaxTransfers) {
		return errors.New("download transfer limits must satisfy 1 <= per-IP <= global")
	}
	if viper.GetInt(HealthPort) < 1 || viper.GetInt(HealthPort) > 65535 {
		return errors.New("download health port must be 1..65535")
	}
	if filepath.Clean(viper.GetString(ManifestPath)) == "." || filepath.Clean(viper.GetString(StorageRoot)) == "." {
		return errors.New("download manifest and storage paths must not be blank")
	}
	return nil
}
