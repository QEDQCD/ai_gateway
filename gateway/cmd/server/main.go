package main

import (
	"github.com/liwenjian/ai_gateway/gateway/internal/config"
	apphttp "github.com/liwenjian/ai_gateway/gateway/internal/http"
	"github.com/liwenjian/ai_gateway/gateway/internal/telemetry"
)

func main() {
	cfg := config.Load()
	logger := telemetry.NewLogger()

	logger.Fatal(apphttp.NewRouter().Listen(cfg.ListenAddr))
}
