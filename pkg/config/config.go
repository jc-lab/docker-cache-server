package config

import (
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Config holds the configuration for the docker cache server
type Config struct {
	Http    HttpConfig    `koanf:"http"`
	Storage StorageConfig `koanf:"storage"`
	Auth    AuthConfig    `koanf:"auth"`
	Cache   CacheConfig   `koanf:"cache"`
}

// HttpConfig holds server-specific configuration
type HttpConfig struct {
	Addr   string `koanf:"addr"`
	Prefix string `koanf:"prefix"`
	// Host e.g. "http://myregistryaddress.org:5000
	Host         string          `koanf:"host"`
	Relativeurls bool            `koanf:"relativeurls"`
	Debug        HttpDebugConfig `koanf:"debug"`
}

type HttpDebugConfig struct {
	Addr       string           `koanf:"addr"`
	Prometheus PrometheusConfig `koanf:"prometheus"`
}

type PrometheusConfig struct {
	Enabled bool   `koanf:"enabled"`
	Path    string `yaml:"path,omitempty"`
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
		Http: HttpConfig{
			Addr:   "0.0.0.0:5000",
			Prefix: "/",
			Debug: HttpDebugConfig{
				Addr: "127.0.0.1:5001",
				Prometheus: PrometheusConfig{
					Enabled: true,
				},
			},
		},
		Storage: StorageConfig{
			Directory: "/var/cache/docker-cache-server",
		},
		Auth: AuthConfig{
			Enabled: false,
			Users:   []UserCreds{},
		},
		Cache: CacheConfig{
			TTL:             7 * 24 * time.Hour, // 7 days
			CleanupInterval: 1 * time.Hour,      // 1 hour
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
	cfg := DefaultConfig()

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
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return cfg, nil
}
