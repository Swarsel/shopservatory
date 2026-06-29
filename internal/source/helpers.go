package source

import (
	"regexp"
	"strings"
)

var nonDigits = regexp.MustCompile(`[^0-9]`)

func absoluteURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	return base + "/" + strings.TrimPrefix(ref, "/")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lastPathSegment(href string) string {
	parts := strings.Split(strings.Trim(href, "/"), "/")
	if len(parts) == 0 {
		return href
	}
	last := parts[len(parts)-1]
	if i := strings.IndexAny(last, "?#"); i >= 0 {
		last = last[:i]
	}
	return last
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
