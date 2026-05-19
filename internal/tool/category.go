package tool

// Category groups tools by what they do. Used by the two-stage tool routing
// path for small local models: round 1 picks a category, round 2 only sees
// schemas in that category.
type Category string

const (
	// CategoryRead — read filesystem state (fs.read, fs.ls).
	CategoryRead Category = "read"
	// CategoryWrite — modify filesystem state (fs.write, fs.edit).
	CategoryWrite Category = "write"
	// CategorySearch — search filesystem content (fs.grep, fs.glob).
	CategorySearch Category = "search"
	// CategoryExec — execute external commands (bash).
	CategoryExec Category = "exec"
	// CategoryMeta — agent orchestration, introspection, and result handling.
	// Default for tools that don't declare a category.
	CategoryMeta Category = "meta"
)

// AllCategories returns the canonical category list in stable order.
func AllCategories() []Category {
	return []Category{CategoryRead, CategoryWrite, CategorySearch, CategoryExec, CategoryMeta}
}

// IsValidCategory reports whether c is one of the known categories.
func IsValidCategory(c Category) bool {
	switch c {
	case CategoryRead, CategoryWrite, CategorySearch, CategoryExec, CategoryMeta:
		return true
	}
	return false
}

// Categorized is the optional interface a tool implements to declare its
// category. Tools that don't implement it fall back to CategoryMeta.
type Categorized interface {
	Category() Category
}

// CategoryOf returns the tool's declared category, or CategoryMeta if the
// tool does not implement Categorized.
func CategoryOf(t Tool) Category {
	if c, ok := t.(Categorized); ok {
		cat := c.Category()
		if IsValidCategory(cat) {
			return cat
		}
	}
	return CategoryMeta
}
