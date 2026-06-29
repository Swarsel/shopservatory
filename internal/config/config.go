package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   Server   `toml:"server"`
	Database Database `toml:"database"`
	Log      Log      `toml:"log"`
	User     User     `toml:"user"`
	Scrape   Scrape   `toml:"scrape"`
	Ebay     Ebay     `toml:"ebay"`
	Telegram Telegram `toml:"telegram"`
	Currency Currency `toml:"currency"`
	OIDC     OIDC     `toml:"oidc"`
	Monitor  Monitor  `toml:"monitor"`
}

type Monitor struct {
	DefaultInterval Duration `toml:"default_interval"`
}

type OIDC struct {
	Issuer   string `toml:"issuer"`
	ClientID string `toml:"client_id"`
}

func (o OIDC) Enabled() bool { return o.Issuer != "" }

type Currency struct {
	Target string `toml:"target"`
}

type User struct {
	Name  string `toml:"name"`
	Email string `toml:"email"`
}

type Server struct {
	Listen string `toml:"listen"`

	BaseURL string `toml:"base_url"`

	ForwardedUserHeader string `toml:"forwarded_user_header"`
}

type Database struct {
	Path string `toml:"path"`
}

type Log struct {
	Level string `toml:"level"`
}

type Scrape struct {
	UserAgent string `toml:"user_agent"`

	DefaultInterval Duration `toml:"default_interval"`

	ProxyURL string `toml:"proxy_url"`

	Timeout Duration `toml:"timeout"`

	BrowserPath string `toml:"browser_path"`

	BrowserTimeout Duration `toml:"browser_timeout"`

	BrowserProxy string `toml:"browser_proxy"`

	FlareSolverrURL string `toml:"flaresolverr_url"`

	FlareSolverrTimeout Duration `toml:"flaresolverr_timeout"`
}

func (s Scrape) BrowserEnabled() bool { return s.BrowserPath != "" }

type Ebay struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`

	Marketplace string `toml:"marketplace"`
}

type Telegram struct {
	Token  string `toml:"token"`
	ChatID string `toml:"chat_id"`
}

func (t Telegram) Enabled() bool { return t.Token != "" }

func (e Ebay) Configured() bool { return e.ClientID != "" && e.ClientSecret != "" }

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

func Default() Config {
	return Config{
		Server:   Server{Listen: "127.0.0.1:8080", ForwardedUserHeader: "X-Forwarded-Email"},
		Database: Database{Path: "shopservatory.db"},
		Log:      Log{Level: "info"},
		User:     User{Name: "admin", Email: "admin@localhost"},
		Scrape: Scrape{
			UserAgent:           "Mozilla/5.0 (X11; Linux x86_64) shopservatory/0.1",
			DefaultInterval:     Duration{5 * time.Minute},
			Timeout:             Duration{30 * time.Second},
			BrowserTimeout:      Duration{45 * time.Second},
			FlareSolverrTimeout: Duration{60 * time.Second},
		},
		Ebay:     Ebay{Marketplace: "EBAY_US"},
		Currency: Currency{Target: "EUR"},
		Monitor:  Monitor{DefaultInterval: Duration{time.Hour}},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode config %q: %w", path, err)
		}
	}

	applyEnvOverrides(&cfg)
	applyDefaults(&cfg)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SHOPSERVATORY_TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv("SHOPSERVATORY_TELEGRAM_CHAT_ID"); v != "" {
		cfg.Telegram.ChatID = v
	}
	if v := os.Getenv("SHOPSERVATORY_EBAY_CLIENT_ID"); v != "" {
		cfg.Ebay.ClientID = v
	}
	if v := os.Getenv("SHOPSERVATORY_EBAY_CLIENT_SECRET"); v != "" {
		cfg.Ebay.ClientSecret = v
	}
	if v := os.Getenv("SHOPSERVATORY_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("SHOPSERVATORY_CHROMIUM"); v != "" {
		cfg.Scrape.BrowserPath = v
	}
	if v := os.Getenv("SHOPSERVATORY_FLARESOLVERR_URL"); v != "" {
		cfg.Scrape.FlareSolverrURL = v
	}
	if v := os.Getenv("SHOPSERVATORY_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("SHOPSERVATORY_OIDC_ISSUER"); v != "" {
		cfg.OIDC.Issuer = v
	}
	if v := os.Getenv("SHOPSERVATORY_OIDC_CLIENT_ID"); v != "" {
		cfg.OIDC.ClientID = v
	}
}

func applyDefaults(cfg *Config) {
	def := Default()
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = def.Server.Listen
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = def.Database.Path
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = def.Log.Level
	}
	if cfg.Scrape.UserAgent == "" {
		cfg.Scrape.UserAgent = def.Scrape.UserAgent
	}
	if cfg.Scrape.DefaultInterval.Duration == 0 {
		cfg.Scrape.DefaultInterval = def.Scrape.DefaultInterval
	}
	if cfg.Scrape.Timeout.Duration == 0 {
		cfg.Scrape.Timeout = def.Scrape.Timeout
	}
	if cfg.Scrape.BrowserTimeout.Duration == 0 {
		cfg.Scrape.BrowserTimeout = def.Scrape.BrowserTimeout
	}
	if cfg.Scrape.FlareSolverrTimeout.Duration == 0 {
		cfg.Scrape.FlareSolverrTimeout = def.Scrape.FlareSolverrTimeout
	}
	if cfg.Ebay.Marketplace == "" {
		cfg.Ebay.Marketplace = def.Ebay.Marketplace
	}
	if cfg.User.Name == "" {
		cfg.User.Name = def.User.Name
	}
	if cfg.User.Email == "" {
		cfg.User.Email = def.User.Email
	}
	if cfg.Server.ForwardedUserHeader == "" {
		cfg.Server.ForwardedUserHeader = def.Server.ForwardedUserHeader
	}
	if cfg.Monitor.DefaultInterval.Duration == 0 {
		cfg.Monitor.DefaultInterval = def.Monitor.DefaultInterval
	}
}

func (c Config) validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen must not be empty")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path must not be empty")
	}
	return nil
}
