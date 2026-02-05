package config

import (
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Config holds the configuration for the docker cache server
type Config struct {
	Server  ServerConfig  `koanf:"server"`
	Storage StorageConfig `koanf:"storage"`
	Auth    AuthConfig    `koanf:"auth"`
	Cache   CacheConfig   `koanf:"cache"`
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	Address string `koanf:"address"`
	Port    int    `koanf:"port"`
}

// StorageConfig holds storage-specific configuration
type StorageConfig struct {
	Directory string `koanf:"directory"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled bool        `koanf:"enabled"`
	Users   []UserCreds `koanf:"users"`
}

// UserCreds holds username and password for a user
type UserCreds struct {
	Username string `koanf:"username"`
	Password string `koanf:"password"`
}

// CacheConfig holds cache-specific configuration
type CacheConfig struct {
	TTL             time.Duration `koanf:"ttl"`
	CleanupInterval time.Duration `koanf:"cleanup_interval"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address: "0.0.0.0",
			Port:    5000,
		},
		Storage: StorageConfig{
			Directory: "/var/cache/docker-cache-server",
		},
		Auth: AuthConfig{
			Enabled: true,
			Users: []UserCreds{
				{Username: "admin", Password: "admin123"},
				{Username: "user1", Password: "password1"},
			},
		},
		Cache: CacheConfig{
			TTL:             30 * 24 * time.Hour, // 30 days
			CleanupInterval: 1 * time.Hour,       // 1 hour
		},
	}
}

// Load loads configuration from various sources in order of precedence:
// 1. Command line flags (highest priority)
// 2. Environment variables
// 3. Config file
// 4. Default values (lowest priority)
func Load(configFile string, flags *pflag.FlagSet) (*Config, error) {
	k := koanf.New(".")

	// Load default config first
	defaultCfg := DefaultConfig()
	defaultMap := map[string]interface{}{
		"server.address":         defaultCfg.Server.Address,
		"server.port":            defaultCfg.Server.Port,
		"storage.directory":      defaultCfg.Storage.Directory,
		"auth.enabled":           defaultCfg.Auth.Enabled,
		"auth.users":             defaultCfg.Auth.Users,
		"cache.ttl":              defaultCfg.Cache.TTL,
		"cache.cleanup_interval": defaultCfg.Cache.CleanupInterval,
	}
	if err := k.Load(confmap.Provider(defaultMap, "."), nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	// Load config file if provided
	if configFile != "" {
		if err := k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("loading config file: %w", err)
		}
	}

	// Load environment variables (prefix: DCS_)
	// e.g., DCS_SERVER_PORT=8080
	if err := k.Load(env.Provider("DCS_", ".", func(s string) string {
		// Convert DCS_SERVER_PORT to server.port
		return s[4:] // Remove DCS_ prefix
	}), nil); err != nil {
		return nil, fmt.Errorf("loading environment variables: %w", err)
	}

	// Load command line flags (highest priority)
	if flags != nil {
		if err := k.Load(posflag.Provider(flags, ".", k), nil); err != nil {
			return nil, fmt.Errorf("loading flags: %w", err)
		}
	}

	// Unmarshal into config struct
	cfg := &Config{}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return cfg, nil
}

// BindFlags binds configuration flags to pflag.FlagSet
func BindFlags(flags *pflag.FlagSet) {
	flags.String("config", "config.yaml", "Path to config file")
	flags.String("server.address", "0.0.0.0", "Server bind address")
	flags.Int("server.port", 5000, "Server port")
	flags.String("storage.directory", "/var/cache/docker-cache-server", "Storage directory")
	flags.Bool("auth.enabled", true, "Enable authentication")
	flags.Duration("cache.ttl", 168*time.Hour, "Cache TTL (e.g., 30d, 720h)")
	flags.Duration("cache.cleanup_interval", 1*time.Hour, "Cleanup interval (e.g., 1h, 60m)")
}
