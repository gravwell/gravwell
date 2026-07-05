package types

var AllowedOperations = []string{"=", "!=", "<>", ">", "<", ">=", "<=", "~"}

type QueryOptions struct {
	Type string // Specifies the type of asset to return, 'mixed' for everything. Ignored except on the /api/list (ListAll) endpoint

	IncludeDeleted bool
	Version        int // fetch a particular version, when appropriate. 0 means latest, -1 means all versions (list only)

	// If true and requesting user is an admin, a list request will return items for all users
	AdminMode bool

	// Listing options
	OrderBy        string // Sort by this field (defaults to ID)
	OrderDirection string
	CursorID       string // Return assets whose ID is greater than the given ID.
	Limit          int    // Max number of assets to return

	// Filtering on fields of assets
	Filters []Filter
}

// Filter based on the values given, e.g. Key = "Name", Operation = "=", Values = ["foo", "bar"].
// Specifying multiple values is an implicit OR.
type Filter struct {
	Key       string
	Operation string
	Values    []any
}

// AvailableFilter defines a filter which *could* be applied: a key, valid operations, and optionally a label.
type AvailableFilter struct {
	Key        string
	Label      string
	Operations []string
}
