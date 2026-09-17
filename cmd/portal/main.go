package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/assets"
	"github.com/Slqzeer/homelab-portal/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(os.Environ())
	if err != nil {
		logger.Error("portal configuration rejected", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           http.FileServer(http.FS(assets.FS())),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("portal listening", "port", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("portal stopped", "error", err)
		os.Exit(1)
	}
}
