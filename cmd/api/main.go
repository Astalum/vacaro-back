package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // Embed IANA time zone data for minimal production images.

	"github.com/astalum/vacaro-back/internal/auth"
	"github.com/astalum/vacaro-back/internal/config"
	"github.com/astalum/vacaro-back/internal/database"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/handler"
	appmiddleware "github.com/astalum/vacaro-back/internal/middleware"
	"github.com/astalum/vacaro-back/internal/repository"
	"github.com/astalum/vacaro-back/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelConnect()

	db, err := database.Open(connectCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	slog.Info("connected to database")

	sessions, err := auth.NewManager(cfg.SessionKey, cfg.SessionCookieSecure)
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}
	oauthStates, err := auth.NewOAuthStateManager(cfg.SessionKey, cfg.SessionCookieSecure)
	if err != nil {
		return fmt.Errorf("create OAuth state manager: %w", err)
	}
	store := repository.New(db)
	application := service.New(store, cfg.GuestTTL, cfg.PublicAppURL)
	if cfg.Google.Enabled {
		tokenBox, err := auth.NewBox(cfg.Google.TokenEncryptionKey)
		if err != nil {
			return fmt.Errorf("create Google token cipher: %w", err)
		}
		googleProvider := googleclient.New(googleclient.Config{
			ClientID:            cfg.Google.ClientID,
			ClientSecret:        cfg.Google.ClientSecret,
			LoginRedirectURL:    cfg.Google.LoginRedirectURL,
			CalendarRedirectURL: cfg.Google.CalendarRedirectURL,
		})
		application.ConfigureGoogle(googleProvider, tokenBox, cfg.RegisteredTTL)
	}

	mux := http.NewServeMux()
	handler.NewHealth(db).Register(mux)
	handler.NewAPI(application, sessions, oauthStates, cfg.PublicAppURL).Register(mux)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           appmiddleware.Chain(mux, appmiddleware.RequestID, appmiddleware.AccessLog, appmiddleware.Recover),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("API server started", "address", server.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownCtx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	slog.Info("API server stopped")
	return nil
}
