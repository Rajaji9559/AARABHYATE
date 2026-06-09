package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aarabhyate/backend/internal/config"
	"github.com/aarabhyate/backend/internal/database"
	"github.com/aarabhyate/backend/internal/handlers"
	"github.com/aarabhyate/backend/internal/repository"
	"github.com/aarabhyate/backend/internal/router"
)

func main() {
	// ── Structured logger ────────────────────────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// ── Config ───────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// ── Database ─────────────────────────────────────────────────────────────
	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database connection established")

	// ── Dependency injection ──────────────────────────────────────────────────
	repos := repository.NewRepositories(db)
	h := handlers.NewHandlers(repos, cfg)

	// ── Router ────────────────────────────────────────────────────────────────
	r := router.New(h, cfg)

	// ── HTTP server ────────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run in goroutine so we can listen for shutdown signals
	go func() {
		slog.Info("AARABHYATE API server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "err", err)
	}
	slog.Info("server exited cleanly")
}
