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

	// Filtering by permissions
	OwnerID           int32   `json:"owner_id"`
	GlobalRead        bool    `json:"global_read"`
	NotGlobalRead     bool    `json:"not_global_read"`
	GlobalWrite       bool    `json:"global_write"`
	NotGlobalWrite    bool    `json:"not_global_write"`
	IncludesReadGIDs  []int32 `json:"includes_read_gids"`
	IncludesWriteGIDs []int32 `json:"includes_write_gids"`

	// Filtering on other fields
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
