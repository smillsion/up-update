package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"up-update.local/up-update/internal/web"
)

type App struct {
	cfg        Config
	db         *sql.DB
	vault      *Vault
	logger     *slog.Logger
	bili       *BilibiliClient
	bark       *BarkClient
	login      *loginLimiter
	deliveryMu sync.Mutex
	qrMu       sync.Mutex
	qrSessions map[string]*biliQRSession
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	db, err := openDB(cfg)
	if err != nil {
		return nil, err
	}
	vault, err := NewVault(cfg.EncryptionKey)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &App{
		cfg: cfg, db: db, vault: vault, logger: logger,
		bili: NewBilibiliClient(cfg.BilibiliBaseURL),
		bark: NewBarkClient(), login: newLoginLimiter(),
		qrSessions: make(map[string]*biliQRSession),
	}, nil
}

func (a *App) Close() error { return a.db.Close() }

func (a *App) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(middleware.Timeout(35 * time.Second))
	r.Use(a.securityHeaders)
	r.Get("/healthz", a.health)
	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", a.loginHandler)
		r.Group(func(r chi.Router) {
			r.Use(a.authenticate)
			r.Get("/auth/me", a.meHandler)
			r.With(a.requireCSRF).Post("/auth/logout", a.logoutHandler)
			r.With(a.requireCSRF).Put("/auth/password", a.changePasswordHandler)
			r.Group(func(r chi.Router) {
				r.Use(a.passwordChanged)
				r.Get("/settings", a.getSettingsHandler)
				r.With(a.requireCSRF).Put("/settings/bilibili", a.saveBilibiliHandler)
				r.With(a.requireCSRF).Post("/settings/bilibili/qrcode", a.startBilibiliQRHandler)
				r.With(a.requireCSRF).Post("/settings/bilibili/qrcode/{id}/poll", a.pollBilibiliQRHandler)
				r.With(a.requireCSRF).Delete("/settings/bilibili/qrcode/{id}", a.cancelBilibiliQRHandler)
				r.With(a.requireCSRF).Put("/settings/bark", a.saveBarkHandler)
				r.With(a.requireCSRF).Post("/settings/bark/test", a.testBarkHandler)
				r.Get("/subscriptions", a.listSubscriptionsHandler)
				r.With(a.requireCSRF).Post("/subscriptions", a.createSubscriptionHandler)
				r.Get("/subscriptions/followings", a.listFollowingsHandler)
				r.With(a.requireCSRF).Post("/subscriptions/import-followings", a.importFollowingsHandler)
				r.With(a.requireCSRF).Patch("/subscriptions/{id}", a.updateSubscriptionHandler)
				r.With(a.requireCSRF).Delete("/subscriptions/{id}", a.deleteSubscriptionHandler)
				r.Get("/deliveries", a.listDeliveriesHandler)
				r.With(a.requireCSRF).Delete("/deliveries/{id}", a.deletePendingDeliveryHandler)
				r.Route("/admin", func(r chi.Router) {
					r.Use(a.requireAdmin)
					r.Get("/users", a.listUsersHandler)
					r.With(a.requireCSRF).Post("/users", a.createUserHandler)
					r.With(a.requireCSRF).Patch("/users/{id}", a.updateUserHandler)
					r.With(a.requireCSRF).Post("/users/{id}/reset-password", a.resetPasswordHandler)
					r.Get("/system", a.getSystemHandler)
					r.With(a.requireCSRF).Put("/system", a.updateSystemHandler)
				})
			})
		})
	})
	r.Handle("/*", web.Handler())
	return r
}

func (a *App) StartWorkers(ctx context.Context) {
	go a.runPollWorker(ctx)
	go a.runDeliveryWorker(ctx)
	go a.runMaintenance(ctx)
}
