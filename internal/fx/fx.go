package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Converter struct {
	target string
	http   *http.Client
	log    *slog.Logger

	mu    sync.RWMutex
	rates map[string]float64
}

func New(target string, log *slog.Logger) *Converter {
	return &Converter{
		target: strings.ToUpper(strings.TrimSpace(target)),
		http:   &http.Client{Timeout: 15 * time.Second},
		log:    log,
		rates:  map[string]float64{},
	}
}

func (c *Converter) Target() string { return c.target }

func (c *Converter) Enabled() bool { return c.target != "" }

func (c *Converter) Run(ctx context.Context) {
	if !c.Enabled() {
		return
	}
	if err := c.refresh(ctx); err != nil {
		c.log.Warn("fx: initial rate fetch failed; conversions unavailable for now", "err", err)
	}
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.refresh(ctx); err != nil {
				c.log.Warn("fx: rate refresh failed", "err", err)
			}
		}
	}
}

func (c *Converter) refresh(ctx context.Context) error {
	url := "https://api.frankfurter.dev/v1/latest?base=" + c.target
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}
	var out struct {
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if len(out.Rates) == 0 {
		return fmt.Errorf("no rates returned")
	}
	c.mu.Lock()
	c.rates = out.Rates
	c.mu.Unlock()
	c.log.Info("fx: refreshed rates", "base", c.target, "currencies", len(out.Rates))
	return nil
}

func (c *Converter) Convert(amount float64, currency string) (float64, bool) {
	if !c.Enabled() {
		return 0, false
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return 0, false
	}
	if currency == c.target {
		return amount, true
	}
	c.mu.RLock()
	rate := c.rates[currency]
	c.mu.RUnlock()
	if rate <= 0 {
		return 0, false
	}
	return amount / rate, true
}

func (c *Converter) Format(amount float64, currency string) string {
	v, ok := c.Convert(amount, currency)
	if !ok || strings.EqualFold(currency, c.target) {
		return ""
	}
	return fmt.Sprintf("≈ %s%.0f", symbol(c.target), v)
}

func symbol(code string) string {
	switch strings.ToUpper(code) {
	case "EUR":
		return "€"
	case "USD":
		return "$"
	case "JPY":
		return "¥"
	case "GBP":
		return "£"
	default:
		return code + " "
	}
}
