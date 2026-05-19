package tool

import (
	"slices"
	"testing"
)

type categorizedStub struct {
	stubTool
	cat Category
}

func (c *categorizedStub) Category() Category { return c.cat }

func TestCategoryOf_DefaultIsMeta(t *testing.T) {
	plain := &stubTool{name: "plain"}
	if got := CategoryOf(plain); got != CategoryMeta {
		t.Errorf("CategoryOf(plain) = %q, want %q", got, CategoryMeta)
	}
}

func TestCategoryOf_DeclaredCategory(t *testing.T) {
	cases := []struct {
		name string
		cat  Category
	}{
		{"read", CategoryRead},
		{"write", CategoryWrite},
		{"search", CategorySearch},
		{"exec", CategoryExec},
		{"meta", CategoryMeta},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &categorizedStub{stubTool: stubTool{name: tc.name}, cat: tc.cat}
			if got := CategoryOf(s); got != tc.cat {
				t.Errorf("CategoryOf(%s) = %q, want %q", tc.name, got, tc.cat)
			}
		})
	}
}

func TestCategoryOf_InvalidFallsBackToMeta(t *testing.T) {
	s := &categorizedStub{stubTool: stubTool{name: "bogus"}, cat: Category("not-a-real-category")}
	if got := CategoryOf(s); got != CategoryMeta {
		t.Errorf("CategoryOf(invalid) = %q, want %q", got, CategoryMeta)
	}
}

func TestIsValidCategory(t *testing.T) {
	for _, c := range AllCategories() {
		if !IsValidCategory(c) {
			t.Errorf("IsValidCategory(%q) = false, want true", c)
		}
	}
	if IsValidCategory(Category("")) {
		t.Error("empty category should be invalid")
	}
	if IsValidCategory(Category("nope")) {
		t.Error("unknown category should be invalid")
	}
}

func TestAllCategoriesStable(t *testing.T) {
	want := []Category{CategoryRead, CategoryWrite, CategorySearch, CategoryExec, CategoryMeta}
	got := AllCategories()
	if !slices.Equal(got, want) {
		t.Errorf("AllCategories() = %v, want %v", got, want)
	}
}
