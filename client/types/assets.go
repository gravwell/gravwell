package types

var AllowedOperations = []string{"=", ">", "<"}

type QueryOptions struct {
	IncludeDeleted bool
	Version        int // fetch a particular version, when appropriate. 0 means latest, -1 means all versions (list only)

	// If true and requesting user is an admin, a list request will return items for all users
	AdminMode bool

	// Listing options
	CursorID string // Return assets whose ID is greater than the given ID.
	Limit    int    // Max number of assets to return

	// Filtering by permissions
	GlobalRead        bool
	NotGlobalRead     bool
	GlobalWrite       bool
	NotGlobalWrite    bool
	IncludesReadGIDs  []int32
	IncludesWriteGIDs []int32

	// Filtering on other fields
	Where []Where
}

type Where struct {
	Key       string
	Operation string
	Value     any
}
