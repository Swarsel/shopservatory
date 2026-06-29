package browser

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

type Browser struct {
	execPath  string
	userAgent string
	timeout   time.Duration
	proxy     string
	log       *slog.Logger
}

func New(execPath, userAgent string, timeout time.Duration, proxy string, log *slog.Logger) *Browser {
	if execPath == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &Browser{execPath: execPath, userAgent: userAgent, timeout: timeout, proxy: proxy, log: log}
}

type RenderOptions struct {
	WaitSelector string

	SettleDelay time.Duration
}

func (b *Browser) RenderHTML(ctx context.Context, rawURL string, opts RenderOptions) (string, error) {

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(b.execPath),
		chromedp.UserAgent(b.userAgent),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", "ja-JP,ja,en-US,en"),
		chromedp.WindowSize(1366, 1800),
	)
	if b.proxy != "" {
		allocOpts = append(allocOpts, chromedp.ProxyServer(b.proxy))
	}

	runCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(runCtx, allocOpts...)
	defer cancelAlloc()
	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	settle := opts.SettleDelay
	if settle <= 0 {
		settle = 2 * time.Second
	}

	var html string
	actions := []chromedp.Action{chromedp.Navigate(rawURL)}
	if opts.WaitSelector != "" {
		sel := opts.WaitSelector
		waitBudget := b.timeout / 2
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			wctx, cancel := context.WithTimeout(ctx, waitBudget)
			defer cancel()
			if err := chromedp.WaitVisible(sel, chromedp.ByQuery).Do(wctx); err != nil && b.log != nil {
				b.log.Debug("browser: wait selector not found, capturing anyway", "url", rawURL, "selector", sel)
			}
			return nil
		}))
	}
	actions = append(actions, chromedp.Sleep(settle), chromedp.OuterHTML("html", &html, chromedp.ByQuery))

	if err := chromedp.Run(taskCtx, actions...); err != nil {
		return "", fmt.Errorf("browser render %s: %w", rawURL, err)
	}
	b.dump(rawURL, html)
	return html, nil
}

func (b *Browser) dump(rawURL, html string) {
	dir := os.Getenv("SHOPSERVATORY_DUMP_DIR")
	if dir == "" {
		return
	}
	name := fmt.Sprintf("%x.html", sha1.Sum([]byte(rawURL)))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		if b.log != nil {
			b.log.Warn("browser dump failed", "path", path, "err", err)
		}
		return
	}
	if b.log != nil {
		b.log.Info("browser dump written", "url", rawURL, "path", path)
	}
}
