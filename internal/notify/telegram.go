package notify

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
	"github.com/Swarsel/shopservatory/internal/store"
)

type Telegram struct {
	token  string
	http   *http.Client
	images *http.Client
}

func NewTelegram(token string) *Telegram {
	if token == "" {
		return nil
	}
	return &Telegram{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

var geoBlockedImageHosts = []string{"yimg.jp", "yahoo.co.jp"}

func geoBlockedImage(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range geoBlockedImageHosts {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func (t *Telegram) SetImageProxy(proxyURL string) {
	if proxyURL == "" {
		return
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return
	}
	t.images = &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
	}
}

func (t *Telegram) Kind() string { return "telegram" }

func (t *Telegram) Send(ctx context.Context, target store.NotificationTarget, ev Event) error {
	chatID := target.Config["chat_id"]
	if chatID == "" {
		return fmt.Errorf("telegram target %d missing chat_id", target.ID)
	}

	caption := t.format(ev)

	if ev.Listing.ImageURL != "" {
		if t.images != nil && geoBlockedImage(ev.Listing.ImageURL) {
			if err := t.uploadPhoto(ctx, chatID, ev.Listing.ImageURL, caption); err == nil {
				return nil
			}
		}
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
	if by := source.BuyeeURL(ev.Listing.Source, ev.Listing.ExternalID); by != "" {
		fmt.Fprintf(&b, "🛫 <a href=\"%s\">buy via Buyee</a>\n", html.EscapeString(by))
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

func (t *Telegram) uploadPhoto(ctx context.Context, chatID, imageURL, caption string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", telegramImageUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")
	if u, uErr := url.Parse(imageURL); uErr == nil {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	}
	resp, err := t.images.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: fetch image: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("telegram: fetch image: content-type %q", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("telegram: fetch image: empty body")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("chat_id", chatID)
	_ = mw.WriteField("caption", caption)
	_ = mw.WriteField("parse_mode", "HTML")
	part, err := mw.CreateFormFile("photo", photoFilename(imageURL))
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", t.token)
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upResp, err := t.http.Do(upReq)
	if err != nil {
		return err
	}
	defer upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(upResp.Body, 1<<16))
		return fmt.Errorf("telegram sendPhoto upload: %s: %s", upResp.Status, string(raw))
	}
	return nil
}

const telegramImageUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func photoFilename(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			if i := strings.IndexByte(base, '?'); i >= 0 {
				base = base[:i]
			}
			if strings.Contains(base, ".") {
				return base
			}
		}
	}
	return "photo.jpg"
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
