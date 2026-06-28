package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type craigslist struct {
	client  *Client
	mu      sync.Mutex
	areaIDs map[string]int
}

func newCraigslist(client *Client) *craigslist {
	return &craigslist{client: client, areaIDs: map[string]int{}}
}

func (c *craigslist) ID() string          { return "craigslist" }
func (c *craigslist) DisplayName() string { return "Craigslist" }

var clCatMap = map[int]string{
	5: "for", 7: "sys", 20: "wan", 42: "bar", 44: "tix", 68: "bik", 69: "mcy",
	73: "gms", 92: "bks", 93: "spo", 94: "clo", 95: "clt", 96: "ele", 97: "hsh",
	98: "msg", 101: "zip", 107: "bab", 117: "emd", 118: "tls", 119: "boa", 120: "jwl",
	122: "pts", 124: "rvs", 132: "tag", 133: "grd", 134: "bfs", 135: "art", 136: "mat",
	137: "pho", 141: "fuo", 142: "fud", 145: "cto", 146: "ctd", 149: "app", 150: "atq",
	151: "vgm", 152: "hab", 153: "mob", 160: "mcd", 162: "ppd", 164: "bod", 165: "mod",
	167: "eld", 168: "rvd", 171: "bad", 172: "bid", 174: "bfd", 175: "emq", 178: "grq",
	179: "fod", 184: "msd", 185: "phd", 187: "tld", 188: "tad", 190: "wad", 191: "snw",
	193: "hvo", 194: "hvd", 195: "mpo", 197: "bop", 199: "sop", 201: "bpo", 203: "wto",
	205: "tro", 206: "trb", 208: "avo",
}

var clAreaID = regexp.MustCompile(`"areaId":(\d+)`)

func (c *craigslist) Search(ctx context.Context, spec SearchSpec) ([]Listing, error) {
	subdomain, query, err := clTarget(spec)
	if err != nil {
		return nil, err
	}
	areaID, err := c.areaID(ctx, subdomain)
	if err != nil {
		return nil, fmt.Errorf("craigslist: resolve area %q: %w", subdomain, err)
	}

	q := url.Values{}
	q.Set("batch", strconv.Itoa(areaID)+"-0-360-0-0")
	q.Set("cc", "US")
	q.Set("lang", "en")
	q.Set("searchPath", "sss")
	q.Set("query", query)
	q.Set("sort", "date")
	endpoint := "https://sapi.craigslist.org/web/v8/postings/search/full?" + q.Encode()

	body, err := c.client.GetBody(ctx, endpoint, map[string]string{
		"Accept":  "*/*",
		"Referer": "https://www.craigslist.org/",
	})
	if err != nil {
		return nil, fmt.Errorf("craigslist: fetch: %w", err)
	}
	return clDecode(body, subdomain, spec)
}

func (c *craigslist) areaID(ctx context.Context, subdomain string) (int, error) {
	c.mu.Lock()
	if id, ok := c.areaIDs[subdomain]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	body, err := c.client.GetBody(ctx, "https://"+subdomain+".craigslist.org/", map[string]string{
		"Accept": "text/html,application/xhtml+xml",
	})
	if err != nil {
		return 0, err
	}
	m := clAreaID.FindSubmatch(body)
	if m == nil {
		return 0, fmt.Errorf("areaId not found")
	}
	id, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.areaIDs[subdomain] = id
	c.mu.Unlock()
	return id, nil
}

func clTarget(spec SearchSpec) (subdomain, query string, err error) {
	if strings.HasPrefix(spec.Query, "http") {
		u, perr := url.Parse(spec.Query)
		if perr != nil {
			return "", "", fmt.Errorf("craigslist: parse url: %w", perr)
		}
		host := u.Hostname()
		label := strings.SplitN(host, ".", 2)[0]
		if label != "" && label != "www" && strings.Contains(host, "craigslist") {
			subdomain = label
		} else {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			for i, p := range parts {
				if p == "area" && i+1 < len(parts) {
					subdomain = parts[i+1]
				}
			}
		}
		query = u.Query().Get("query")
		if query == "" {
			query = u.Query().Get("q")
		}
		if subdomain == "" {
			return "", "", fmt.Errorf("craigslist: could not determine region from url")
		}
		return subdomain, query, nil
	}

	subdomain = spec.Param("region")
	if subdomain == "" {
		return "", "", fmt.Errorf("craigslist: a region is required (set the 'region' param, e.g. sfbay, or paste a full craigslist search url)")
	}
	return subdomain, spec.Query, nil
}

func clDecode(body []byte, searchSubdomain string, spec SearchSpec) ([]Listing, error) {
	var resp struct {
		Data struct {
			Items  []json.RawMessage `json:"items"`
			Decode struct {
				MinPostingID int64             `json:"minPostingId"`
				Locations    []json.RawMessage `json:"locations"`
			} `json:"decode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("craigslist: decode json: %w", err)
	}

	type clLoc struct{ subdomain, subarea string }
	locs := make([]clLoc, len(resp.Data.Decode.Locations))
	for i, raw := range resp.Data.Decode.Locations {
		var a []interface{}
		if json.Unmarshal(raw, &a) == nil && len(a) >= 3 {
			sub, _ := a[1].(string)
			sa, _ := a[2].(string)
			locs[i] = clLoc{sub, sa}
		}
	}

	minID := resp.Data.Decode.MinPostingID
	listings := make([]Listing, 0, len(resp.Data.Items))
	for _, raw := range resp.Data.Items {
		var arr []interface{}
		if json.Unmarshal(raw, &arr) != nil || len(arr) < 5 {
			continue
		}
		idDelta, _ := arr[0].(float64)
		postID := int64(idDelta) + minID
		catID := 0
		if f, ok := arr[2].(float64); ok {
			catID = int(f)
		}
		geo, _ := arr[4].(string)

		title, slug, priceStr, imageToken := "", "", "", ""
		for _, e := range arr {
			switch v := e.(type) {
			case string:
				title = v
			case []interface{}:
				if len(v) >= 2 {
					code, _ := v[0].(float64)
					switch int(code) {
					case 4:
						imageToken, _ = v[1].(string)
					case 6:
						slug, _ = v[1].(string)
					case 10:
						priceStr, _ = v[1].(string)
					}
				}
			}
		}

		price := clPrice(priceStr)
		if !withinPriceBounds(spec, price) {
			continue
		}

		subdomain, subarea := searchSubdomain, ""
		if i := clLocIndex(geo); i >= 0 && i < len(locs) && locs[i].subdomain != "" {
			subdomain, subarea = locs[i].subdomain, locs[i].subarea
		}

		listings = append(listings, Listing{
			ExternalID: strconv.FormatInt(postID, 10),
			Title:      title,
			Price:      price,
			Currency:   "USD",
			URL:        clURL(subdomain, subarea, clCatMap[catID], slug, postID),
			ImageURL:   clImage(imageToken),
			Extra: map[string]string{
				"location": subarea,
			},
		})
	}
	return listings, nil
}

func clLocIndex(geo string) int {
	if geo == "" {
		return -1
	}
	head := geo
	if i := strings.IndexByte(head, '~'); i >= 0 {
		head = head[:i]
	}
	first := strings.SplitN(head, ":", 2)[0]
	idx, err := strconv.Atoi(first)
	if err != nil {
		return -1
	}
	return idx
}

func clURL(subdomain, subarea, catAbbr, slug string, postID int64) string {
	base := "https://" + subdomain + ".craigslist.org"
	id := strconv.FormatInt(postID, 10)
	if catAbbr == "" || slug == "" {
		return base
	}
	if subarea != "" {
		return base + "/" + subarea + "/" + catAbbr + "/d/" + slug + "/" + id + ".html"
	}
	return base + "/" + catAbbr + "/d/" + slug + "/" + id + ".html"
}

func clImage(token string) string {
	if token == "" {
		return ""
	}
	if i := strings.IndexByte(token, ':'); i >= 0 {
		token = token[i+1:]
	}
	return "https://images.craigslist.org/" + token + "_600x450.jpg"
}

func clPrice(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' || r == '.' {
			b.WriteRune(r)
		}
	}
	v, _ := strconv.ParseFloat(b.String(), 64)
	return v
}
