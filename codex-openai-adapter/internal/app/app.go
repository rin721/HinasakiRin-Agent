package app

import (
	"fmt"

	"codex-openai-adapter/internal/middleware"
	"codex-openai-adapter/internal/modules/chat"
	"codex-openai-adapter/internal/modules/codex"
	appconfig "codex-openai-adapter/internal/modules/config"

	"github.com/gin-gonic/gin"
)

// App 是整个 HTTP 服务的组合根。
//
// 学习重点：
// main.go 只负责加载配置和启动；真正的依赖组装放在 app.New。
// 这样可以把“启动入口”和“业务模块怎么连起来”分开。
type App struct {
	engine *gin.Engine
	config appconfig.Config
}

// New 创建 Gin engine，并把配置、Codex runner、HTTP handler、中间件接起来。
func New(cfg appconfig.Config) (*App, error) {
	// Runner 是和 Codex CLI 打交道的底层模块。
	// 如果 safe_workdir 配错，这里会尽早失败，服务不会带着危险配置启动。
	runner, err := codex.NewRunner(cfg.Codex)
	if err != nil {
		return nil, err
	}

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery(), middleware.LocalOnlyHeaders())

	// /health 不放在 /v1 下面，也不需要 Authorization。
	// 它只回答服务是否活着，不暴露模型或 Codex 能力。
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// 所有 OpenAI-compatible 路由都放在 /v1 group 下，并统一加 Bearer 鉴权。
	v1 := engine.Group("/v1", middleware.BearerAuth(cfg.Gateway.APIToken))
	chat.NewHandler(runner, chat.HandlerOptions{
		DefaultModel:  cfg.Codex.DefaultModel,
		MaxImages:     cfg.Codex.MaxImages,
		MaxImageBytes: cfg.Codex.MaxImageBytes,
	}).Register(v1)

	return &App{
		engine: engine,
		config: cfg,
	}, nil
}

// Run 只监听 127.0.0.1，避免默认暴露到局域网。
// 这个 adapter 的定位是 local-only 学习服务，不是生产网关。
func (a *App) Run() error {
	return a.engine.Run(fmt.Sprintf("127.0.0.1:%d", a.config.Server.Port))
}

// Engine 暴露 Gin engine，主要方便测试或未来嵌入其它启动方式。
func (a *App) Engine() *gin.Engine {
	return a.engine
}
