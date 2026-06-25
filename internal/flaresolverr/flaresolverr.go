package flaresolverr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	endpoint   string
	http       *http.Client
	maxTimeout time.Duration
	proxy      string
}

func New(baseURL string, maxTimeout time.Duration, proxy string) *Client {
	if baseURL == "" {
		return nil
	}
	if maxTimeout <= 0 {
		maxTimeout = 60 * time.Second
	}
	return &Client{
		endpoint:   strings.TrimRight(baseURL, "/") + "/v1",
		http:       &http.Client{Timeout: maxTimeout + 20*time.Second},
		maxTimeout: maxTimeout,
		proxy:      proxy,
	}
}

func (c *Client) Get(ctx context.Context, rawURL string) (string, error) {
	reqMap := map[string]any{
		"cmd":        "request.get",
		"url":        rawURL,
		"maxTimeout": c.maxTimeout.Milliseconds(),
	}
	if c.proxy != "" {
		reqMap["proxy"] = map[string]any{"url": c.proxy}
	}
	reqBody, err := json.Marshal(reqMap)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("flaresolverr request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("flaresolverr: status %s", resp.Status)
	}

	var out struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Status   int    `json:"status"`
			Response string `json:"response"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("flaresolverr: decode: %w", err)
	}
	if out.Status != "ok" {
		return "", fmt.Errorf("flaresolverr: %s", out.Message)
	}
	if out.Solution.Status < 200 || out.Solution.Status >= 400 {
		return "", fmt.Errorf("flaresolverr: upstream status %d", out.Solution.Status)
	}
	return out.Solution.Response, nil
}
