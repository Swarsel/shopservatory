package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Swarsel/shopservatory/internal/auth"
	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/fx"
	"github.com/Swarsel/shopservatory/internal/notify"
	"github.com/Swarsel/shopservatory/internal/scheduler"
	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
	"github.com/Swarsel/shopservatory/internal/web"
)

func main() {

	if len(os.Args) > 1 && os.Args[1] == "probe" {
		if err := probe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "shopservatory probe:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shopservatory:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to TOML config file (optional)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log := newLogger(cfg.Log.Level)
	log.Info("starting shopservatory", "listen", cfg.Server.Listen, "db", cfg.Database.Path)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	user, err := st.EnsureDefaultUser(ctx, cfg.User.Name, cfg.User.Email)
	if err != nil {
		return fmt.Errorf("ensure default user: %w", err)
	}
	log.Info("default user", "id", user.ID, "email", user.Email)

	if cfg.Telegram.Enabled() && cfg.Telegram.ChatID != "" {
		if err := st.EnsureTelegramTarget(ctx, user.ID, cfg.Telegram.ChatID); err != nil {
			return fmt.Errorf("provision telegram target: %w", err)
		}
		log.Info("telegram target provisioned", "chat_id", cfg.Telegram.ChatID)
	}

	client, err := source.NewClient(cfg.Scrape, log)
	if err != nil {
		return fmt.Errorf("build http client: %w", err)
	}
	registry := source.NewRegistry(cfg, client, log)
	log.Info("sources registered", "ids", registry.IDs())

	conv := fx.New(cfg.Currency.Target, log)
	go conv.Run(ctx)
	if conv.Enabled() {
		log.Info("currency conversion enabled", "target", conv.Target())
	}

	tg := notify.NewTelegram(cfg.Telegram.Token)
	notifier := notify.NewManager(log, conv, tg)
	if !cfg.Telegram.Enabled() {
		log.Warn("Telegram disabled: no bot token configured (dashboard feed still works)")
	}

	sched := scheduler.New(st, registry, notifier, log, scheduler.Options{
		DefaultInterval: cfg.Scrape.DefaultInterval.Duration,
	})

	authn, err := auth.New(ctx, st, cfg.OIDC.Issuer, cfg.OIDC.ClientID, cfg.Server.ForwardedUserHeader, user.ID, log)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}
	if authn.OIDCEnabled() {
		log.Info("OIDC enabled", "issuer", cfg.OIDC.Issuer)
	} else {
		log.Warn("OIDC disabled: API and dashboard fall back to the default user")
	}

	srv := web.New(st, registry, sched, conv, authn, cfg.Monitor.DefaultInterval.Duration, log)

	var wg sync.WaitGroup
	wg.Add(2)

	var schedErr, webErr error
	go func() {
		defer wg.Done()
		if err := sched.Run(ctx); err != nil && err != context.Canceled {
			schedErr = err
			stop()
		}
	}()
	go func() {
		defer wg.Done()
		if err := web.Serve(ctx, cfg.Server.Listen, srv.Handler(), log); err != nil {
			webErr = err
			stop()
		}
	}()

	wg.Wait()
	log.Info("shopservatory stopped")
	if schedErr != nil {
		return schedErr
	}
	return webErr
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
