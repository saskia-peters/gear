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
	admcore "github.com/saskia-peters/gear/internal/admin/core"
	admbck "github.com/saskia-peters/gear/internal/admin/adapters/backup"
	adminhttp "github.com/saskia-peters/gear/internal/admin/adapters/http"
	adminpostgres "github.com/saskia-peters/gear/internal/admin/adapters/postgres"
	admsmtp "github.com/saskia-peters/gear/internal/admin/adapters/smtp"
	userhttp "github.com/saskia-peters/gear/internal/user/adapters/http"
	userpostgres "github.com/saskia-peters/gear/internal/user/adapters/postgres"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// resetEmailStub was the placeholder ResetEmailSender for Story 1.8 (FR-26).
// Story 3.1 replaces it with the real SMTP sender built from the Admin
// settings port (see main); the port contract stays unchanged.

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

	// Story 3.1 + 3.2 + 4.1 — materialized Admin hexagon for the settings
	// surfaces (FR-28/FR-29/FR-30/AD-1): the Admin-owned smtp_settings +
	// backup_destinations + schedules stores (AD-11/AD-14/AD-15/AD-16), the
	// settings core and the settings HTTP handlers. The core consumes the User
	// module's repository READ-ONLY for the permission re-check (AD-12) and
	// the audit trail (NFR-O1/NFR-O2, audit_log is User-owned) — the Admin
	// module never authors another module's SQL (AD-8/AD-11).
	adminStore := adminpostgres.New(pool)
	adminRepo := adminpostgres.NewRepository(adminStore)
	adminSettingsService := admcore.NewService(adminRepo, adminRepo, adminRepo, secretCipher, userRepo, userRepo, admsmtp.Client{}, admbck.NewTester(), log)
	adminSettingsHandler := adminhttp.NewHandler(adminSettingsService, log)

	// Password reset email delivery (FR-26/AD-14): Story 3.1 wires the REAL
	// SMTP sender (built below from the Admin settings port), replacing the
	// Epic-1 stub. Configured() is true only when a working server is
	// configured, so the must-change-password fallback stays active otherwise —
	// no behavioral regression when unconfigured.
	userService.SetResetEmailSender(admsmtp.NewResetEmailSender(adminSettingsService, secretCipher, log))
	// Reset links are built from the public app origin (GEAR_APP_ORIGIN, review
	// finding 1.8-6) so a real sender can deliver a clickable link.
	userService.SetAppOrigin(cfg.AppOrigin)
	userHandler := userhttp.NewHandler(userService, log, sessionManager)

	// The auth gateway resolves sessions and the live permission set (AD-6).
	// The ADMIN module's outer gate is ANY admin-module code (Spec 2.9 /
	// Effort 2): fuehrende/schirrmeister hold `users.view` +
	// `users.qualifications.manage` and must reach the user directory + the
	// qualification assignment on the user detail, so the outer gate can no
	// longer be `admin.recovery.approve`-only. Holding ANY of
	// usercore.AdminModuleAccessCodes opens the module; each sub-surface then
	// applies its own tighter gate (e.g. recovery still requires
	// `admin.recovery.approve`, user create/edit requires `users.manage`).
	adminSurface := auth.RequireAnyPermission(sessionManager, userRepo, usercore.AdminModuleAccessCodes(), "admin access denied", log)(userHandler.AdminRoutes())

	// The settings surface is a sibling sub-mount under /api/v1/admin with its
	// OWN tighter gate: only holders of `admin.settings.email` reach it (AD-6);
	// the core re-checks the same code defense-in-depth.
	settingsSurface := auth.RequireAnyPermission(sessionManager, userRepo, []string{admcore.SmtpSettingsPermission}, "admin.settings.email access denied", log)(adminSettingsHandler.Routes())

	// The backup-destination surface mounts under /api/v1/admin/settings/backup
	// with its OWN gate — one permission per surface (AD-6): only holders of
	// `admin.settings.backup` reach it. The core re-checks the same code
	// defense-in-depth. It deliberately does NOT widen the SMTP gate above.
	backupSurface := auth.RequireAnyPermission(sessionManager, userRepo, []string{admcore.BackupSettingsPermission}, "admin.settings.backup access denied", log)(adminSettingsHandler.BackupRoutes())

	// The schedule-catalog surface mounts under
	// /api/v1/admin/settings/schedules with its OWN gate — one permission per
	// surface (AD-6/AD-16): only holders of `schedules.manage` reach it. The
	// core re-checks the same code defense-in-depth. It deliberately does NOT
	// widen the SMTP/backup gates above.
	schedulesSurface := auth.RequireAnyPermission(sessionManager, userRepo, []string{admcore.SchedulesPermission}, "schedules.manage access denied", log)(adminSettingsHandler.ScheduleRoutes())

	// Demo route for the gateway composition tests: any active user holding
	// `dashboard.view` (all base roles) can reach /api/v1/protected/me.
	protectedRoute := auth.Route(sessionManager, userRepo, "dashboard.view")

	log.Info("wired user repository, sessions and registration/auth service", "store", fmt.Sprintf("%T", userStore))

	r := router.New(pool, log,
		router.WithAuth(userHandler.Routes()),
		router.WithProtected(protectedRoute),
		router.WithMount("/api/v1/admin", adminSurface),
		router.WithMount("/api/v1/admin/settings", settingsSurface),
		router.WithMount("/api/v1/admin/settings/backup", backupSurface),
		router.WithMount("/api/v1/admin/settings/schedules", schedulesSurface),
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
