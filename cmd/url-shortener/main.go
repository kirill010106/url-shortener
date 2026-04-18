package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
	ssogrpc "url-shortener/internal/clients/sso/grpc"
	"url-shortener/internal/config"
	"url-shortener/internal/http-server/handlers/del"
	"url-shortener/internal/http-server/handlers/redirect"
	"url-shortener/internal/http-server/handlers/url/save"
	authmw "url-shortener/internal/http-server/middleware/auth"
	"url-shortener/internal/lib/logger/handlers/slogpretty"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage/postgres"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error while loading .env file")
	}
	cfg := config.MustLoad()

	log := setupLogger(cfg.Env)

	log.Info("starting url-shortener", slog.String("env", cfg.Env))
	log.Debug("debug messages are enabled")
	if cfg.AppSecret == "" {
		log.Error("app secret is empty, set APP_SECRET or app_secret in config")
		os.Exit(1)
	}

	if cfg.AppID <= 0 {
		log.Error("app id must be greater than zero", slog.Int64("app_id", cfg.AppID))
		os.Exit(1)
	}

	if cfg.Clients.SSO.Address == "" {
		log.Error("sso address is empty, set clients.sso.address in config")
		os.Exit(1)
	}

	if cfg.Clients.SSO.Timeout <= 0 {
		cfg.Clients.SSO.Timeout = 3 * time.Second
	}

	ssoDialCtx, cancelSSODial := context.WithTimeout(context.Background(), cfg.Clients.SSO.Timeout)
	defer cancelSSODial()

	ssoClient, err := ssogrpc.New(
		ssoDialCtx,
		log,
		cfg.Clients.SSO.Address,
		cfg.Clients.SSO.Timeout,
		cfg.Clients.SSO.RetriesCount,
	)
	if err != nil {
		log.Error("failed to init sso client", sl.Err(err))
		os.Exit(1)
	}
	defer func() {
		if err := ssoClient.Close(); err != nil {
			log.Warn("failed to close sso client", sl.Err(err))
		}
	}()

	DBUrl := os.Getenv("DATABASE_URL")
	storage, err := postgres.New(DBUrl)
	if err != nil {
		log.Error("failed to init storage", sl.Err(err))
		os.Exit(1)
	}
	defer storage.Close()

	router := chi.NewRouter()

	//middleware

	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.URLFormat)
	jwtAuthMiddleware := authmw.New(log, cfg.AppSecret, cfg.AppID)
	requireAuth := authmw.RequireAuth(log)
	requireAdmin := authmw.RequireAdmin(log, ssoClient)

	router.Route("/url", func(r chi.Router) {
		r.Use(jwtAuthMiddleware)

		r.With(requireAuth).Post("/", save.New(log, storage))
		r.With(requireAdmin).Delete("/{alias}", del.New(log, storage))

	})
	router.Get("/{alias}", redirect.New(log, storage))
	// TODO: return all db(new handler with admin)

	log.Info("starting server", slog.String("address", cfg.Address))
	srv := &http.Server{
		Addr:         cfg.Address,
		Handler:      router,
		ReadTimeout:  cfg.HTTPServer.Timeout,
		WriteTimeout: cfg.HTTPServer.Timeout,
		IdleTimeout:  cfg.HTTPServer.IdleTimeout,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Error("failed to start server")
	}
	log.Error("server stopped")
}

func setupLogger(env string) *slog.Logger {
	var log *slog.Logger
	switch env {
	case envLocal:
		log = setupPrettySlog()
	case envDev:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case envProd:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		)
	}
	return log
}

func setupPrettySlog() *slog.Logger {
	opts := slogpretty.PrettyHandlerOptions{
		SlogOpts: &slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
	}

	handler := opts.NewPrettyHandler(os.Stdout)

	return slog.New(handler)
}
