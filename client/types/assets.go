package types

var AllowedOperations = []string{"=", "!=", "<>", ">", "<", ">=", "<=", "~"}

type QueryOptions struct {
	IncludeDeleted bool `json:"include_deleted"`
	Version        int  `json:"version"` // fetch a particular version, when appropriate. 0 means latest, -1 means all versions (list only)

	// If true and requesting user is an admin, a list request will return items for all users
	AdminMode bool `json:"admin_mode"`

	// Listing options
	OrderBy        string `json:"order_by"` // Sort by this field (defaults to ID)
	OrderDirection string `json:"order_direction"`
	CursorID       string `json:"cursor"`    // Return assets whose ID is greater than the given ID.
	Limit          int    `json:"page_size"` // Max number of assets to return

	// Filtering on fields of assets
	Filters []Filter `json:"filters"`
}

// Filter based on the values given, e.g. Key = "Name", Operation = "=", Values = ["foo", "bar"].
// Specifying multiple values is an implicit OR.
type Filter struct {
	Key       string `json:"key"`
	Operation string `json:"operation"`
	Values    []any  `json:"values"`
}

// AvailableFilter defines a filter which *could* be applied: a key, valid operations, and optionally a label.
type AvailableFilter struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Operations []string `json:"operations"`
}
