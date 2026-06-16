package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"

	"github.com/hezimy/PrismCast/internal/applog"
)

const DefaultVolume = 100

// Config holds the application configuration
type Config struct {
	DeviceName        string `json:"device_name"`
	DeviceUUID        string `json:"device_uuid"`
	PlayerPath        string `json:"player_path"`
	AutoStart         bool   `json:"auto_start"`
	ImageViewerFirst  bool   `json:"image_viewer_first"`
	Volume            int    `json:"volume"`
	Language          string `json:"language"`
	Theme             string `json:"theme"`
	LogLevel          string `json:"log_level"`
	VerboseLog        bool   `json:"verbose_log,omitempty"` // 已废弃，仅用于迁移
}

// Default returns a default configuration
func Default() *Config {
	return &Config{
		DeviceName:       getDefaultDeviceName(),
		DeviceUUID:       uuid.New().String(),
		PlayerPath:       getDefaultPlayerPath(),
		AutoStart:        false,
		ImageViewerFirst: true,
		Volume:           DefaultVolume,
		Language:         "zh-CN",
		Theme:            "dark",
		LogLevel:         "main",
	}
}

// Load loads configuration from file
func Load() (*Config, error) {
	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			_ = Save(cfg)
			return cfg, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Validate and set defaults for missing fields
	if cfg.DeviceName == "" {
		cfg.DeviceName = getDefaultDeviceName()
	}
	if cfg.DeviceUUID == "" {
		cfg.DeviceUUID = uuid.New().String()
	}
	if cfg.PlayerPath == "" {
		cfg.PlayerPath = getDefaultPlayerPath()
	}
	oldVol := cfg.Volume
	cfg.Volume = NormalizeVolume(cfg.Volume)
	cfg.LogLevel = applog.NormalizeLevel(cfg.LogLevel, cfg.VerboseLog)
	cfg.VerboseLog = false
	if oldVol != cfg.Volume {
		if err := Save(&cfg); err != nil {
			log.Printf("[Config] 保存音量设置失败: %v", err)
		} else {
			log.Printf("[Config] 默认音量已从 %d 更新为 %d", oldVol, cfg.Volume)
		}
	}

	return &cfg, nil
}

// NormalizeVolume 校验并规范化音量（旧版默认 50 会升级到 100）
func NormalizeVolume(vol int) int {
	if vol <= 0 || vol > 100 {
		return DefaultVolume
	}
	if vol == 50 {
		return DefaultVolume
	}
	return vol
}

// Save saves configuration to file
func Save(cfg *Config) error {
	path := getConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func getConfigPath() string {
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	case "darwin":
		dir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support")
	default:
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}
	return filepath.Join(dir, "PrismCast", "config.json")
}

func getDefaultDeviceName() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "PrismCast"
	}
	return fmt.Sprintf("%s's PrismCast", hostname)
}

func getDefaultPlayerPath() string {
	switch runtime.GOOS {
	case "windows":
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)

		searchPaths := []string{
			filepath.Join(exeDir, "mpv.exe"),
			"mpv.exe",
			filepath.Join(os.Getenv("ProgramFiles"), "mpv", "mpv.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "mpv", "mpv.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "mpv", "mpv.exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "scoop", "apps", "mpv", "current", "mpv.exe"),
		}
		for _, p := range searchPaths {
			if _, err := os.Stat(p); err == nil {
				log.Printf("[Config] 找到播放器: %s", p)
				return p
			}
		}
		log.Printf("[Config] 未找到mpv，将使用默认路径")
		return filepath.Join(exeDir, "mpv.exe")
	case "darwin":
		return "/usr/local/bin/mpv"
	default:
		return "mpv"
	}
}
