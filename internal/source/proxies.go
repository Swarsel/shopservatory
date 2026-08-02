package source

import (
	"encoding/hex"
	"strings"
)

var doorzoMalls = map[string]string{
	"mercari":          "mercari",
	"surugaya":         "surugaya",
	"paypayfleamarket": "paypay",
	"rakuma":           "rakuma",
	"snkrdunk":         "snkrdunk",
}

var buyeeItemPaths = map[string]string{
	"yahooauctions": "item/yahoo/auction/",
}

func BuyeeURL(sourceID, externalID string) string {
	path, ok := buyeeItemPaths[strings.ToLower(sourceID)]
	if !ok {
		return ""
	}
	id := strings.TrimSpace(externalID)
	if id == "" {
		return ""
	}
	return "https://buyee.jp/" + path + id
}

func DoorzoURL(sourceID, itemURL, externalID string) string {
	mall, ok := doorzoMalls[strings.ToLower(sourceID)]
	if !ok {
		return ""
	}
	native := NativeItemURL(sourceID, itemURL, externalID)
	if native == "" {
		return ""
	}
	return "https://www.doorzo.com/en/mall/" + mall + "/detail/" + hex.EncodeToString([]byte(native))
}

func NativeItemURL(sourceID, itemURL, externalID string) string {
	id := strings.TrimSpace(externalID)
	switch strings.ToLower(sourceID) {
	case "paypayfleamarket":
		if id == "" {
			return ""
		}
		return "https://paypayfleamarket.yahoo.co.jp/item/" + id
	case "yahooauctions":
		if id == "" {
			return ""
		}
		return "https://page.auctions.yahoo.co.jp/jp/auction/" + id
	}
	u := strings.TrimSpace(itemURL)
	if u == "" {
		return ""
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return u
}
