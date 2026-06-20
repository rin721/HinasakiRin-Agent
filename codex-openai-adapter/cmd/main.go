package main

import (
	"log"

	"codex-openai-adapter/internal/app"
	appconfig "codex-openai-adapter/internal/modules/config"
)

func main() {
	// 启动顺序保持朴素，方便学习：
	// 1. 读取 config.yaml / 环境变量。
	// 2. 创建 App，内部会组装 Gin、Runner、Handler、中间件。
	// 3. 监听 127.0.0.1:<port>。
	cfg, err := appconfig.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	log.Printf("codex-openai-adapter listening on http://localhost:%d", cfg.Server.Port)
	if err := application.Run(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
