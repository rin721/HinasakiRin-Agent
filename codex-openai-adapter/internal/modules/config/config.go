package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 是 config.yaml 的根结构。
// 配置分三组：HTTP 服务、网关鉴权、Codex CLI 执行参数。
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Gateway GatewayConfig `mapstructure:"gateway"`
	Codex   CodexConfig   `mapstructure:"codex"`
}

// ServerConfig 控制本地 HTTP 服务。
type ServerConfig struct {
	Port int `mapstructure:"port"`
}

// GatewayConfig 控制 OpenAI-compatible 网关层。
// APIToken 对应客户端请求中的 Authorization: Bearer <token>。
type GatewayConfig struct {
	APIToken string `mapstructure:"api_token"`
}

// CodexConfig 控制底层 codex exec 的行为。
type CodexConfig struct {
	// SafeWorkdir 是 Codex CLI 的工作目录。
	// 它必须是独立的 codex-workdir，不能直接指向用户代码仓库。
	SafeWorkdir string `mapstructure:"safe_workdir"`
	TimeoutSecs int    `mapstructure:"timeout_seconds"`
	Binary      string `mapstructure:"binary"`
	// DefaultModel 用于请求 model=auto 时的默认模型。
	// 为空表示不传 --model，让 Codex CLI 自己选择默认模型。
	DefaultModel string `mapstructure:"default_model"`
	// ServiceTier 和 ModelReasoningEffort 会映射为 codex exec --config key=value。
	ServiceTier          string `mapstructure:"service_tier"`
	ModelReasoningEffort string `mapstructure:"model_reasoning_effort"`
	// MaxImages / MaxImageBytes 是图片输入的资源限制。
	MaxImages     int   `mapstructure:"max_images"`
	MaxImageBytes int64 `mapstructure:"max_image_bytes"`
}

// Load 负责读取配置、设置默认值、应用环境变量覆盖，并做启动前校验。
//
// 环境变量格式示例：
//
//	CODEX_ADAPTER_GATEWAY_API_TOKEN=local-api-token
//	CODEX_ADAPTER_CODEX_DEFAULT_MODEL=gpt-5.4-mini
//
// 这个函数是服务启动前的“配置关口”：如果关键配置不安全或不完整，直接返回错误。
func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	// Viper 会把 CODEX_ADAPTER_CODEX_SAFE_WORKDIR 映射到 codex.safe_workdir。
	v.SetEnvPrefix("CODEX_ADAPTER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 默认值让项目 clone 下来就能跑，适合学习。
	// 生产系统通常会把这些值外置到部署环境或 secrets 管理里。
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

	// 下面这些校验都属于“越早失败越好”：
	// 配置错误不应该等到第一次 HTTP 请求时才暴露。
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
