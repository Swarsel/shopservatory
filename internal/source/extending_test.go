package source

import (
	"testing"
	"time"
)

func TestMercariAuctionOngoingDetection(t *testing.T) {
	for _, state := range []string{"STATE_ONGOING", "state_ongoing", "State_Ongoing"} {
		if !(mercariAuction{State: state}).ongoing() {
			t.Errorf("%q should count as ongoing", state)
		}
	}
	for _, state := range []string{"", "STATE_ENDED", "STATE_CANCELLED"} {
		if (mercariAuction{State: state}).ongoing() {
			t.Errorf("%q must not count as ongoing", state)
		}
	}
}

func TestMercariExtraMarksExtendingWhenEndTimeDropped(t *testing.T) {
	a := mercariAuction{State: "STATE_ONGOING", TotalBids: 56, ExpectedEndTime: 0}
	extra := a.extra()
	if extra["extending"] != "1" {
		t.Error("an ongoing auction with no published end time must be marked as extending")
	}
	if _, ok := extra["ends"]; ok {
		t.Error("no end time should be published when the api omits it")
	}
	if extra["bids"] != "56" {
		t.Errorf("bids = %q", extra["bids"])
	}
}

func TestMercariExtraKeepsEndTimeWhenPublished(t *testing.T) {
	end := time.Now().Add(2 * time.Hour).Unix()
	a := mercariAuction{State: "STATE_ONGOING", TotalBids: 3, ExpectedEndTime: end}
	extra := a.extra()
	if extra["ends"] == "" {
		t.Fatal("a published end time must be kept")
	}
	if _, ok := extra["extending"]; ok {
		t.Error("an auction with a known end time is not extending")
	}
}

func TestMercariEndedAuctionIsNotExtending(t *testing.T) {
	a := mercariAuction{State: "STATE_ENDED", ExpectedEndTime: 0}
	if _, ok := a.extra()["extending"]; ok {
		t.Error("an ended auction must not be reported as extending")
	}
}
