package source

import (
	"context"
	"testing"
	"time"
)

func TestWillhabenCategoryPathParsing(t *testing.T) {
	got := whCategoryIDs("4390;4525;4552;4554")
	want := []string{"4390", "4525", "4552", "4554"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if leaf := whLeafCategory(got); leaf != "4554" {
		t.Fatalf("leaf = %s", leaf)
	}

	multi := whCategoryIDs("[3928;4257;4266, 4390;4525;4565]")
	if len(multi) != 6 || multi[0] != "3928" || multi[5] != "4565" {
		t.Fatalf("multi-path parse: %v", multi)
	}

	if len(whCategoryIDs("")) != 0 || len(whCategoryIDs("  ")) != 0 {
		t.Fatal("blank must yield no ids")
	}
}

func TestWillhabenParentExcludesDescendants(t *testing.T) {
	cats := whCategoryIDs("3928;4257;4266")
	if !anyCategoryExcluded(SearchSpec{ExcludeCategories: "3928"}, cats) {
		t.Fatal("excluding the root of the path must match")
	}
	if !anyCategoryExcluded(SearchSpec{ExcludeCategories: "4266"}, cats) {
		t.Fatal("excluding the leaf must match")
	}
	if anyCategoryExcluded(SearchSpec{ExcludeCategories: "9999"}, cats) {
		t.Fatal("unrelated id must not match")
	}
	if anyCategoryExcluded(SearchSpec{}, cats) {
		t.Fatal("no exclusions must never match")
	}
}

func TestKleinanzeigenCategoryFromSlug(t *testing.T) {
	got := kleinanzeigenCategory("/s-anzeige/canyon-pathlite-onfly/3462723793-217-9043")
	if got != "217" {
		t.Fatalf("got %q want 217", got)
	}
	for _, bad := range []string{
		"",
		"/s-anzeige/thing/onlyone",
		"/s-anzeige/thing/123-notanumber-456",
		"/s-anzeige/thing/1-2-3-4",
	} {
		if c := kleinanzeigenCategory(bad); c != "" {
			t.Fatalf("%q should not parse, got %q", bad, c)
		}
	}
}

func TestEbayCategoryExclusionUsesWholeChain(t *testing.T) {
	leaf := []string{"183454"}
	chain := []string{"183454", "2536", "220"}
	if !ebayCategoryExcluded(SearchSpec{ExcludeCategories: "220"}, leaf, chain) {
		t.Fatal("a parent in the chain must exclude")
	}
	if !ebayCategoryExcluded(SearchSpec{ExcludeCategories: "183454"}, leaf, chain) {
		t.Fatal("the leaf must exclude")
	}
	if ebayCategoryExcluded(SearchSpec{ExcludeCategories: "999"}, leaf, chain) {
		t.Fatal("unrelated id must not exclude")
	}
	if ebayCategoryExcluded(SearchSpec{}, leaf, chain) {
		t.Fatal("no exclusions must never exclude")
	}
}

func TestAnyCategoryExcludedSkipsBlanks(t *testing.T) {
	if anyCategoryExcluded(SearchSpec{ExcludeCategories: "AUTO_MOTO"}, []string{"", "", ""}) {
		t.Fatal("blank categories must never match")
	}
	if !anyCategoryExcluded(SearchSpec{ExcludeCategories: "BOATS"}, []string{"AUTO_MOTO", "BOATS", ""}) {
		t.Fatal("a populated level must match")
	}
}

func TestRakumaAncestorWalk(t *testing.T) {
	r := &rakuma{}
	r.categories.parent = map[string]string{
		"527": "526",
		"526": "10005",
	}
	r.categories.fetched = time.Now()

	got := r.categories.ancestors(context.Background(), r, "527")
	want := []string{"527", "526", "10005"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	if len(r.categories.ancestors(context.Background(), r, "")) != 0 {
		t.Fatal("blank id must yield nothing")
	}
	orphan := r.categories.ancestors(context.Background(), r, "99999")
	if len(orphan) != 1 || orphan[0] != "99999" {
		t.Fatalf("an unknown id should return just itself, got %v", orphan)
	}
}

func TestRakumaAncestorWalkStopsOnCycle(t *testing.T) {
	r := &rakuma{}
	r.categories.parent = map[string]string{"a": "b", "b": "a"}
	r.categories.fetched = time.Now()
	got := r.categories.ancestors(context.Background(), r, "a")
	if len(got) != 2 {
		t.Fatalf("a cycle must terminate, got %v", got)
	}
}

func TestRakumaParentExclusion(t *testing.T) {
	r := &rakuma{}
	r.categories.parent = map[string]string{"527": "526", "526": "10005"}
	r.categories.fetched = time.Now()
	ctx := context.Background()

	for _, id := range []string{"527", "526", "10005"} {
		if !r.categoryExcluded(ctx, SearchSpec{ExcludeCategories: id}, "527") {
			t.Errorf("excluding %s should hide leaf 527", id)
		}
	}
	if r.categoryExcluded(ctx, SearchSpec{ExcludeCategories: "10001"}, "527") {
		t.Error("an unrelated top-level id must not hide 527")
	}
	if r.categoryExcluded(ctx, SearchSpec{}, "527") {
		t.Error("no exclusions must never hide")
	}
	if r.categoryExcluded(ctx, SearchSpec{ExcludeCategories: "10005"}, "") {
		t.Error("a listing with no category must never be hidden")
	}
}
