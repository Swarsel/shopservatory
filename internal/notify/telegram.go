package notify

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type Telegram struct {
	token string
	http  *http.Client
}

func NewTelegram(token string) *Telegram {
	if token == "" {
		return nil
	}
	return &Telegram{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (t *Telegram) Kind() string { return "telegram" }

func (t *Telegram) Send(ctx context.Context, target store.NotificationTarget, ev Event) error {
	chatID := target.Config["chat_id"]
	if chatID == "" {
		return fmt.Errorf("telegram target %d missing chat_id", target.ID)
	}

	caption := t.format(ev)

	if ev.Listing.ImageURL != "" {
		if err := t.call(ctx, "sendPhoto", url.Values{
			"chat_id":    {chatID},
			"photo":      {ev.Listing.ImageURL},
			"caption":    {caption},
			"parse_mode": {"HTML"},
		}); err == nil {
			return nil
		}

	}
	return t.call(ctx, "sendMessage", url.Values{
		"chat_id":                  {chatID},
		"text":                     {caption},
		"parse_mode":               {"HTML"},
		"disable_web_page_preview": {"false"},
	})
}

func (t *Telegram) format(ev Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🛒 <b>%s</b>\n", html.EscapeString(ev.Source))
	if title := strings.TrimSpace(ev.Listing.Title); title != "" {
		if ev.Listing.URL != "" {
			fmt.Fprintf(&b, "<a href=\"%s\">%s</a>\n", html.EscapeString(ev.Listing.URL), html.EscapeString(title))
		} else {
			fmt.Fprintf(&b, "%s\n", html.EscapeString(title))
		}
	}
	if p := formatPrice(ev.Listing.Price, ev.Listing.Currency); p != "" {
		line := p
		if ev.PriceApprox != "" {
			line += " " + ev.PriceApprox
		}
		fmt.Fprintf(&b, "💴 %s\n", html.EscapeString(line))
	}
	if left := auctionTimeLeft(ev.Listing.Extra["ends"], time.Now()); left != "" {
		fmt.Fprintf(&b, "⏳ %s\n", html.EscapeString(left))
	}
	if dz := source.DoorzoURL(ev.Listing.Source, ev.Listing.URL, ev.Listing.ExternalID); dz != "" {
		fmt.Fprintf(&b, "🛫 <a href=\"%s\">buy via Doorzo</a>\n", html.EscapeString(dz))
	}
	if ev.Note != "" {
		fmt.Fprintf(&b, "%s", html.EscapeString(ev.Note))
		return b.String()
	}
	fmt.Fprintf(&b, "🔎 query: <i>%s</i>", html.EscapeString(ev.Search.Query))
	return b.String()
}

func auctionTimeLeft(ends string, now time.Time) string {
	end, err := time.Parse(time.RFC3339, ends)
	if err != nil {
		return ""
	}
	left := end.Sub(now)
	if left <= 0 {
		return "auction ended"
	}
	days := int(left.Hours()) / 24
	hours := int(left.Hours()) % 24
	mins := int(left.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("ends in %dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("ends in %dh %dm", hours, mins)
	default:
		return fmt.Sprintf("ends in %dm", mins)
	}
}

func (t *Telegram) call(ctx context.Context, method string, form url.Values) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("telegram %s: %s: %s", method, resp.Status, string(body))
	}
	return nil
}
