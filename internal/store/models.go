package store

import (
	"time"

	"github.com/Swarsel/shopservatory/internal/source"
)

type User struct {
	ID           int64
	Name         string
	Email        string
	OIDCSubject  string
	PasswordHash string
	CreatedAt    time.Time
}

type Search struct {
	ID        int64
	UserID    int64
	Source    string
	Query     string
	Params    map[string]string
	MinPrice  *float64
	MaxPrice  *float64
	Interval  time.Duration
	Enabled   bool
	CreatedAt time.Time
	LastRunAt *time.Time
}

func (s Search) Spec() source.SearchSpec {
	return source.SearchSpec{
		Query:    s.Query,
		MinPrice: s.MinPrice,
		MaxPrice: s.MaxPrice,
		Params:   s.Params,
	}
}

type Listing struct {
	ID         int64
	SearchID   int64
	Source     string
	ExternalID string
	Title      string
	Price      float64
	Currency   string
	URL        string
	ImageURL   string
	SaleType   string
	Extra      map[string]string
	FirstSeen  time.Time

	ListedAt time.Time
	Notified bool
}

type MonitoredItem struct {
	ID            int64
	UserID        int64
	Source        string
	ExternalID    string
	URL           string
	Title         string
	ImageURL      string
	Currency      string
	SaleType      string
	LastPrice     float64
	Status        string
	Interval      time.Duration
	Enabled       bool
	CreatedAt     time.Time
	LastCheckedAt *time.Time
}

type PricePoint struct {
	Price      float64
	Status     string
	ObservedAt time.Time
}

type NotificationTarget struct {
	ID        int64
	UserID    int64
	Kind      string
	Config    map[string]string
	Enabled   bool
	CreatedAt time.Time
}
