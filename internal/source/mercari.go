package source

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type mercari struct {
	client    *Client
	searchURL string
}

func newMercari(client *Client) *mercari {
	return &mercari{client: client, searchURL: "https://api.mercari.jp/v2/entities:search"}
}

func (m *mercari) ID() string          { return "mercari" }
func (m *mercari) DisplayName() string { return "Mercari JP" }

func (m *mercari) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	sort, order := "SORT_CREATED_TIME", "ORDER_DESC"
	switch spec.Param("sort") {
	case "price_asc":
		sort, order = "SORT_PRICE", "ORDER_ASC"
	case "price_desc":
		sort, order = "SORT_PRICE", "ORDER_DESC"
	}

	status := []string{"STATUS_ON_SALE"}
	if spec.Param("status") == "all" {
		status = nil
	}

	priceMin, priceMax := 0, 0
	if spec.MinPrice != nil {
		priceMin = int(*spec.MinPrice)
	}
	if spec.MaxPrice != nil {
		priceMax = int(*spec.MaxPrice)
	}

	reqBody := map[string]any{
		"userId":          "",
		"pageSize":        120,
		"searchSessionId": uuidV4(),
		"indexRouting":    "INDEX_ROUTING_UNSPECIFIED",
		"thumbnailTypes":  []string{},
		"searchCondition": map[string]any{
			"keyword":  spec.Query,
			"sort":     sort,
			"order":    order,
			"status":   status,
			"priceMin": priceMin,
			"priceMax": priceMax,
		},
		"defaultDatasets": []string{"DATASET_TYPE_MERCARI", "DATASET_TYPE_BEYOND"},
		"serviceFrom":     "suruga",
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	dpop, err := mercariDPoP(http.MethodPost, m.searchURL)
	if err != nil {
		return nil, fmt.Errorf("mercari: dpop: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.searchURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Platform", "web")
	req.Header.Set("DPoP", dpop)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mercari: search status %s: %s", resp.Status, truncate(body, 300))
	}

	var out struct {
		Items []struct {
			ID         string      `json:"id"`
			Name       string      `json:"name"`
			Price      string      `json:"price"`
			Thumbnails []string    `json:"thumbnails"`
			ItemType   string      `json:"itemType"`
			Created    json.Number `json:"created"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("mercari: decode: %w", err)
	}

	listings := make([]Listing, 0, len(out.Items))
	for _, it := range out.Items {
		price, _ := strconv.ParseFloat(it.Price, 64)
		var thumb string
		if len(it.Thumbnails) > 0 {
			thumb = it.Thumbnails[0]
		}
		var listedAt time.Time
		if sec, err := it.Created.Int64(); err == nil && sec > 0 {
			listedAt = time.Unix(sec, 0)
		}
		listings = append(listings, Listing{
			ExternalID: it.ID,
			Title:      it.Name,
			Price:      price,
			Currency:   "JPY",
			URL:        "https://jp.mercari.com/item/" + it.ID,
			ImageURL:   thumb,
			ListedAt:   listedAt,
		})
	}
	return listings, nil
}

func mercariDPoP(method, htu string) (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}

	header := map[string]any{
		"typ": "dpop+jwt",
		"alg": "ES256",
		"jwk": map[string]any{
			"crv": "P-256",
			"kty": "EC",
			"x":   b64url(key.X.Bytes()),
			"y":   b64url(key.Y.Bytes()),
		},
	}
	claims := map[string]any{
		"iat":  time.Now().Unix(),
		"jti":  uuidV4(),
		"htu":  htu,
		"htm":  method,
		"uuid": uuidV4(),
	}

	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url(hb) + "." + b64url(cb)

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}

	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + b64url(sig), nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func uuidV4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
