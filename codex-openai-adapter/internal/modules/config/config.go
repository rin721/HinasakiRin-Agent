package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Gateway GatewayConfig `mapstructure:"gateway"`
	Codex   CodexConfig   `mapstructure:"codex"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type GatewayConfig struct {
	APIToken string `mapstructure:"api_token"`
}

type CodexConfig struct {
	SafeWorkdir          string `mapstructure:"safe_workdir"`
	TimeoutSecs          int    `mapstructure:"timeout_seconds"`
	Binary               string `mapstructure:"binary"`
	DefaultModel         string `mapstructure:"default_model"`
	ServiceTier          string `mapstructure:"service_tier"`
	ModelReasoningEffort string `mapstructure:"model_reasoning_effort"`
	MaxImages            int    `mapstructure:"max_images"`
	MaxImageBytes        int64  `mapstructure:"max_image_bytes"`
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	v.SetEnvPrefix("CODEX_ADAPTER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.port", 8788)
	v.SetDefault("gateway.api_token", "local-api-token")
	v.SetDefault("codex.safe_workdir", "./codex-workdir")
	v.SetDefault("codex.timeout_seconds", 120)
	v.SetDefault("codex.binary", "codex")
	v.SetDefault("codex.default_model", "")
	v.SetDefault("codex.service_tier", "fast")
	v.SetDefault("codex.model_reasoning_effort", "")
	v.SetDefault("codex.max_images", 10)
	v.SetDefault("codex.max_image_bytes", 20*1024*1024)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return Config{}, fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Gateway.APIToken == "" {
		return Config{}, fmt.Errorf("gateway.api_token is required")
	}
	if cfg.Codex.SafeWorkdir == "" {
		return Config{}, fmt.Errorf("codex.safe_workdir is required")
	}
	if cfg.Codex.TimeoutSecs <= 0 {
		return Config{}, fmt.Errorf("codex.timeout_seconds must be positive")
	}
	if cfg.Codex.Binary == "" {
		return Config{}, fmt.Errorf("codex.binary is required")
	}
	if cfg.Codex.MaxImages < 0 {
		return Config{}, fmt.Errorf("codex.max_images must be zero or positive")
	}
	if cfg.Codex.MaxImageBytes <= 0 {
		return Config{}, fmt.Errorf("codex.max_image_bytes must be positive")
	}

	return cfg, nil
}
