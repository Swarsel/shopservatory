package source

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	rakumaTopLevelIDs = []string{
		"10001", "10002", "10003", "10004", "10005", "10006", "10007",
		"10008", "10009", "10010", "10011", "10012", "10013", "10014",
	}
	rakumaQueryIDRe = regexp.MustCompile(`category_id=(\d+)`)
	rakumaPathIDRe  = regexp.MustCompile(`/category/(\d+)`)
)

type rakumaCategoryTree struct {
	mu      sync.Mutex
	parent  map[string]string
	fetched time.Time
}

func (t *rakumaCategoryTree) ancestors(ctx context.Context, r *rakuma, id string) []string {
	if id == "" {
		return nil
	}
	parent := t.load(ctx, r)
	out := []string{id}
	seen := map[string]bool{id: true}
	cur := id
	for i := 0; i < 8; i++ {
		up, ok := parent[cur]
		if !ok || up == "" || seen[up] {
			break
		}
		seen[up] = true
		out = append(out, up)
		cur = up
	}
	return out
}

func (t *rakumaCategoryTree) load(ctx context.Context, r *rakuma) map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.parent != nil && time.Since(t.fetched) < 24*time.Hour {
		return t.parent
	}
	parent, err := r.crawlCategories(ctx)
	if err != nil || len(parent) == 0 {
		if t.parent != nil {
			return t.parent
		}
		return map[string]string{}
	}
	t.parent = parent
	t.fetched = time.Now()
	return parent
}

type rakumaLink struct{ child, parent string }

func (r *rakuma) crawlCategories(ctx context.Context) (map[string]string, error) {
	var mu sync.Mutex
	parent := map[string]string{}
	record := func(child, up string) {
		mu.Lock()
		if _, exists := parent[child]; !exists {
			parent[child] = up
		}
		mu.Unlock()
	}

	expand := func(id string, kids []string) []rakumaLink {
		out := make([]rakumaLink, 0, len(kids))
		for _, k := range kids {
			if k != id {
				out = append(out, rakumaLink{k, id})
			}
		}
		return out
	}

	mids := r.fanOut(ctx, rakumaTopLevelIDs, expand, record)
	r.fanOut(ctx, mids, expand, record)

	if len(parent) == 0 {
		return nil, fmt.Errorf("rakuma: category crawl produced nothing")
	}
	return parent, nil
}

func (r *rakuma) fanOut(
	ctx context.Context,
	ids []string,
	expand func(id string, kids []string) []rakumaLink,
	record func(child, up string),
) []string {
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var discovered []string

	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			kids, err := r.categoryChildren(ctx, id)
			if err != nil {
				return
			}
			for _, l := range expand(id, kids) {
				record(l.child, l.parent)
				mu.Lock()
				discovered = append(discovered, l.child)
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return discovered
}

func (r *rakuma) categoryChildren(ctx context.Context, id string) ([]string, error) {
	body, err := r.client.GetBody(ctx, "https://fril.jp/category?category_id="+id, map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "ja,en;q=0.8",
	})
	if err != nil {
		return nil, err
	}
	page := html.UnescapeString(string(body))

	seen := map[string]bool{}
	var out []string
	add := func(matches [][]string) {
		for _, m := range matches {
			v := m[1]
			if v == "" || v == id || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	if strings.HasPrefix(id, "100") && len(id) == 5 {
		add(rakumaQueryIDRe.FindAllStringSubmatch(page, -1))
	} else {
		add(rakumaPathIDRe.FindAllStringSubmatch(page, -1))
	}
	return out, nil
}

func (r *rakuma) categoryExcluded(ctx context.Context, spec SearchSpec, categoryID string) bool {
	want := spec.ExcludedCategoryIDs()
	if len(want) == 0 || categoryID == "" {
		return false
	}
	for _, w := range want {
		if w == categoryID {
			return true
		}
	}
	for _, c := range r.categories.ancestors(ctx, r, categoryID) {
		for _, w := range want {
			if c == w {
				return true
			}
		}
	}
	return false
}
