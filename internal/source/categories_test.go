package source

import "testing"

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
