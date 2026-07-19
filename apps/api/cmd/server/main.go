package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"full-stack-learning/apps/api/internal/biojob"
	"full-stack-learning/apps/api/internal/config"
	"full-stack-learning/apps/api/internal/database"
	"full-stack-learning/apps/api/internal/httpapi"
	"full-stack-learning/apps/api/internal/profile"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	pool, err := database.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	profileRepository := profile.NewRepository(pool)
	profileService := profile.NewService(profileRepository)
	handler := httpapi.NewHandler(profileService, profileService, profileService, profileService, pool)
	if cfg.BioJobInternalKey != "" {
		jobRepository := biojob.NewRepository(pool)
		handler.ConfigureBioJobs(biojob.NewService(jobRepository), cfg.BioJobInternalKey)
	} else {
		logger.Warn("bio job routes are disabled because BIO_JOB_INTERNAL_KEY is empty")
	}
	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           httpapi.WithCORS(handler.Routes(), cfg.CORSAllowedOrigins),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)
	go func() {
		logger.Info("HTTP server started", "address", server.Addr, "environment", cfg.AppEnv)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("HTTP server stopped")
}
