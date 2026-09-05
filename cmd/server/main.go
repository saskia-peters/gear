// Command server is the G.E.A.R. composition root (AD-1): the only place that
// wires module hexagons and their adapters together and mounts the HTTP
// surface. No business logic lives here — handlers, adapters and repositories
// delegate to the modules.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/config"
	"github.com/saskia-peters/gear/internal/platform/crypto"
	"github.com/saskia-peters/gear/internal/platform/logger"
	"github.com/saskia-peters/gear/internal/platform/router"
	userhttp "github.com/saskia-peters/gear/internal/user/adapters/http"
	userpostgres "github.com/saskia-peters/gear/internal/user/adapters/postgres"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

func main() {
	cfg := config.Load(os.Getenv)
	log := logger.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// AD-1: adapters are constructed here and handed to the hexagons.
	userStore := userpostgres.New(pool)
	userRepo := userpostgres.NewRepository(userStore)
	hasher := crypto.NewHasher()
	// TOTP secret encryption at rest (NFR-S4): the 32-byte key from
	// GEAR_ENCRYPTION_KEY. A missing/invalid key is surfaced as a clear startup
	// warning (MFA will be unavailable and MFA endpoints answer 503
	// "MFA ist derzeit nicht verfügbar.") rather than silently disabling MFA
	// (review finding 1.6-3). A missing key only affects MFA flows, not
	// ordinary login/register.
	encKey, keyErr := cfg.EncryptionKeyBytes()
	if keyErr != nil {
		log.Warn("GEAR_ENCRYPTION_KEY missing or invalid; MFA operations will be unavailable",
			"error", keyErr, "hint", "generate a 32-byte key: openssl rand -hex 32")
	}
	secretCipher := crypto.NewSecretCipher(encKey)
	sessionManager := usercore.NewSessionManager(userRepo, cfg.SessionIdle)
	userService := usercore.NewService(userRepo, hasher, sessionManager, secretCipher, log)
	userHandler := userhttp.NewHandler(userService, log, sessionManager)

	// The auth gateway resolves sessions and the live permission set (AD-6).
	const protectedPermission = "admin.recovery.approve"
	protectedRoute := auth.Route(sessionManager, userRepo, protectedPermission)

	log.Info("wired user repository, sessions and registration/auth service", "store", fmt.Sprintf("%T", userStore))

	r := router.New(pool, log,
		router.WithAuth(userHandler.Routes()),
		router.WithProtected(protectedRoute),
	)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed", "error", err)
		}
	}()

	log.Info("server listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
	log.Info("server stopped")
}
