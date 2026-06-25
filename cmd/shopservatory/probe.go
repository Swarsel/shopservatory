package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Swarsel/shopservatory/internal/config"
	"github.com/Swarsel/shopservatory/internal/source"
)

type paramFlag map[string]string

func (p paramFlag) String() string { return fmt.Sprintf("%v", map[string]string(p)) }

func (p paramFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("param must be key=value, got %q", v)
	}
	p[strings.TrimSpace(k)] = strings.TrimSpace(val)
	return nil
}

func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	configPath := fs.String("config", "", "path to TOML config file (optional)")
	src := fs.String("source", "", "source id (ebay, mercari, snkrdunk, surugaya, paypayfleamarket, willhaben)")
	query := fs.String("query", "", "search query")
	minPrice := fs.Float64("min", -1, "minimum price (optional)")
	maxPrice := fs.Float64("max", -1, "maximum price (optional)")
	limit := fs.Int("limit", 10, "max results to print")
	params := paramFlag{}
	fs.Var(params, "param", "source-specific param key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *src == "" || *query == "" {
		fs.Usage()
		return fmt.Errorf("-source and -query are required")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log := newLogger("warn")
	client, err := source.NewClient(cfg.Scrape, log)
	if err != nil {
		return err
	}
	registry := source.NewRegistry(cfg, client, log)

	s, ok := registry.Get(*src)
	if !ok {
		return fmt.Errorf("source %q is not registered (eBay needs credentials; available: %v)", *src, registry.IDs())
	}

	spec := source.SearchSpec{Query: *query, Params: params}
	if *minPrice >= 0 {
		spec.MinPrice = minPrice
	}
	if *maxPrice >= 0 {
		spec.MaxPrice = maxPrice
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	listings, err := s.Search(ctx, spec)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%s: %d result(s) in %s\n\n", s.DisplayName(), len(listings), time.Since(start).Round(time.Millisecond))
	for i, l := range listings {
		if i >= *limit {
			fmt.Fprintf(os.Stdout, "... and %d more\n", len(listings)-*limit)
			break
		}
		price := ""
		if l.Price != 0 || l.Currency != "" {
			price = fmt.Sprintf(" — %s %.0f", l.Currency, l.Price)
		}
		fmt.Fprintf(os.Stdout, "%2d. %s%s\n    %s\n", i+1, truncateStr(l.Title, 90), price, l.URL)
	}
	return nil
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
