package types

var AllowedOperations = []string{"=", "!=", "<>", ">", "<", ">=", "<=", "~"}

type QueryOptions struct {
	IncludeDeleted bool `json:"include_deleted"`
	Version        int  `json:"version"` // fetch a particular version, when appropriate. 0 means latest, -1 means all versions (list only)

	// If true and requesting user is an admin, a list request will return items for all users
	AdminMode bool `json:"admin_mode"`

	// Listing options
	CursorID string `json:"cursor"`    // Return assets whose ID is greater than the given ID.
	Limit    int    `json:"page_size"` // Max number of assets to return

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

type Filter struct {
	Key       string
	Operation string
	Value     any
}
