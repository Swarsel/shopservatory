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

const base = "EUR"

type Converter struct {
	defaultTarget string
	http          *http.Client
	log           *slog.Logger

	mu    sync.RWMutex
	rates map[string]float64
}

func New(defaultTarget string, log *slog.Logger) *Converter {
	return &Converter{
		defaultTarget: strings.ToUpper(strings.TrimSpace(defaultTarget)),
		http:          &http.Client{Timeout: 15 * time.Second},
		log:           log,
		rates:         map[string]float64{},
	}
}

func (c *Converter) DefaultTarget() string { return c.defaultTarget }

func (c *Converter) Resolve(userCurrency string) string {
	userCurrency = strings.ToUpper(strings.TrimSpace(userCurrency))
	if userCurrency != "" {
		return userCurrency
	}
	return c.defaultTarget
}

func (c *Converter) Run(ctx context.Context) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.frankfurter.dev/v1/latest?base="+base, nil)
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
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if len(out.Rates) == 0 {
		return fmt.Errorf("no rates returned")
	}
	out.Rates[base] = 1
	c.mu.Lock()
	c.rates = out.Rates
	c.mu.Unlock()
	c.log.Info("fx: refreshed rates", "base", base, "currencies", len(out.Rates))
	return nil
}

func (c *Converter) ConvertTo(amount float64, from, to string) (float64, bool) {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == "" || to == "" {
		return 0, false
	}
	if from == to {
		return amount, true
	}
	c.mu.RLock()
	rateFrom, rateTo := c.rates[from], c.rates[to]
	c.mu.RUnlock()
	if rateFrom <= 0 || rateTo <= 0 {
		return 0, false
	}
	return amount / rateFrom * rateTo, true
}

func (c *Converter) FormatFor(amount float64, from, to string) string {
	to = strings.ToUpper(strings.TrimSpace(to))
	if to == "" || strings.EqualFold(from, to) {
		return ""
	}
	v, ok := c.ConvertTo(amount, from, to)
	if !ok {
		return ""
	}
	return fmt.Sprintf("≈ %s%.0f", symbol(to), v)
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
