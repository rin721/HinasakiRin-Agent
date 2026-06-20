package app

import (
	"fmt"

	"codex-openai-adapter/internal/middleware"
	"codex-openai-adapter/internal/modules/chat"
	"codex-openai-adapter/internal/modules/codex"
	appconfig "codex-openai-adapter/internal/modules/config"

	"github.com/gin-gonic/gin"
)

type App struct {
	engine *gin.Engine
	config appconfig.Config
}

func New(cfg appconfig.Config) (*App, error) {
	runner, err := codex.NewRunner(cfg.Codex)
	if err != nil {
		return nil, err
	}

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery(), middleware.LocalOnlyHeaders())

	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

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

func (a *App) Run() error {
	return a.engine.Run(fmt.Sprintf("127.0.0.1:%d", a.config.Server.Port))
}

func (a *App) Engine() *gin.Engine {
	return a.engine
}
