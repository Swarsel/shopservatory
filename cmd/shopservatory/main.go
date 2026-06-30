package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
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

	var seededAny bool
	for _, acc := range cfg.Users {
		if acc.Email == "" || acc.Password == "" {
			log.Warn("skipping seeded user without email/password", "email", acc.Email)
			continue
		}
		hash, herr := auth.HashPassword(acc.Password)
		if herr != nil {
			return fmt.Errorf("hash password for %q: %w", acc.Email, herr)
		}
		u, created, serr := st.SeedUser(ctx, acc.Name, strings.ToLower(acc.Email), hash)
		if serr != nil {
			return fmt.Errorf("seed user %q: %w", acc.Email, serr)
		}
		seededAny = true
		if created {
			if err := st.UpdateUserSettings(ctx, u.ID, acc.Currency, acc.SearchInterval.Duration, acc.MonitorInterval.Duration); err != nil {
				return fmt.Errorf("seed settings for %q: %w", acc.Email, err)
			}
			if acc.TelegramChatID != "" {
				if err := st.SetTelegramChatID(ctx, u.ID, acc.TelegramChatID); err != nil {
					return fmt.Errorf("seed telegram for %q: %w", acc.Email, err)
				}
			}
		}
		if acc.Admin {
			if err := st.SetAdmin(ctx, u.ID, true); err != nil {
				return fmt.Errorf("set admin for %q: %w", acc.Email, err)
			}
		}
		log.Info("seeded login account", "id", u.ID, "email", u.Email, "created", created, "admin", acc.Admin)
	}
	if !seededAny && !cfg.OIDC.Enabled() {
		log.Warn("no login accounts configured and OIDC disabled: nobody can sign in (set [[users]] or [oidc])")
	}

	client, err := source.NewClient(cfg.Scrape, log)
	if err != nil {
		return fmt.Errorf("build http client: %w", err)
	}
	registry := source.NewRegistry(cfg, client, log)
	log.Info("sources registered", "ids", registry.IDs())

	conv := fx.New(cfg.Currency.Target, log)
	go conv.Run(ctx)
	log.Info("currency conversion ready", "default_target", conv.DefaultTarget())

	tg := notify.NewTelegram(cfg.Telegram.Token)
	notifier := notify.NewManager(log, conv, tg)
	if !cfg.Telegram.Enabled() {
		log.Warn("Telegram disabled: no bot token configured (dashboard feed still works)")
	}

	sched := scheduler.New(st, registry, notifier, log, scheduler.Options{
		DefaultInterval: cfg.Scrape.DefaultInterval.Duration,
	})

	authn, err := auth.New(ctx, st, auth.Options{
		Issuer:       cfg.OIDC.Issuer,
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		OIDCName:     cfg.OIDC.Label(),
		BaseURL:      cfg.Server.BaseURL,
	}, log)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}
	if authn.OIDCEnabled() {
		log.Info("OIDC login enabled", "issuer", cfg.OIDC.Issuer)
	}

	imageProxy := cfg.Scrape.ProxyURL
	if imageProxy == "" {
		imageProxy = cfg.Scrape.BrowserProxy
	}
	srv := web.New(st, registry, sched, conv, authn, cfg.Scrape.DefaultInterval.Duration, cfg.Monitor.DefaultInterval.Duration, imageProxy, log)

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
