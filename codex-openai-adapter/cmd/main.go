package main

import (
	"log"

	"codex-openai-adapter/internal/app"
	appconfig "codex-openai-adapter/internal/modules/config"
)

func main() {
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
