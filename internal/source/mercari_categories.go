package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type mercariCategory struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ParentID     string `json:"parentCategoryId"`
	RootID       string `json:"rootCategoryId"`
	RootName     string `json:"rootCategoryName"`
	ParentName   string `json:"parentCategoryName"`
	ShortLabel   string `json:"shortLabel"`
	DisplayOrder string `json:"displayOrder"`
}

type mercariCategoryTree struct {
	mu      sync.Mutex
	byID    map[string]mercariCategory
	fetched time.Time
}

func (t *mercariCategoryTree) ancestors(ctx context.Context, m *mercari, id string) []string {
	if id == "" {
		return nil
	}
	byID := t.load(ctx, m)
	out := []string{id}
	seen := map[string]bool{id: true}
	cur := id
	for i := 0; i < 8; i++ {
		c, ok := byID[cur]
		if !ok {
			break
		}
		for _, up := range []string{c.ParentID, c.RootID} {
			if up != "" && !seen[up] {
				seen[up] = true
				out = append(out, up)
			}
		}
		if c.ParentID == "" || c.ParentID == cur {
			break
		}
		cur = c.ParentID
	}
	return out
}

func (t *mercariCategoryTree) load(ctx context.Context, m *mercari) map[string]mercariCategory {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byID != nil && time.Since(t.fetched) < 24*time.Hour {
		return t.byID
	}
	byID, err := m.fetchCategories(ctx)
	if err != nil {
		if t.byID != nil {
			return t.byID
		}
		return map[string]mercariCategory{}
	}
	t.byID = byID
	t.fetched = time.Now()
	return byID
}

func (m *mercari) fetchCategories(ctx context.Context) (map[string]mercariCategory, error) {
	endpoint := "https://api.mercari.jp/master/v2/datasets/item_categories"
	dpop, err := mercariDPoP(http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Platform", "web")
	req.Header.Set("DPoP", dpop)
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mercari: categories status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		ItemCategories []mercariCategory `json:"itemCategories"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("mercari: decode categories: %w", err)
	}
	byID := make(map[string]mercariCategory, len(out.ItemCategories))
	for _, c := range out.ItemCategories {
		byID[c.ID] = c
	}
	if len(byID) == 0 {
		return nil, fmt.Errorf("mercari: category dataset was empty")
	}
	return byID, nil
}

func (m *mercari) categoryExcluded(ctx context.Context, spec SearchSpec, categoryID string) bool {
	want := spec.ExcludedCategoryIDs()
	if len(want) == 0 || categoryID == "" {
		return false
	}
	chain := m.categories.ancestors(ctx, m, categoryID)
	for _, c := range chain {
		for _, w := range want {
			if c == w {
				return true
			}
		}
	}
	return false
}
