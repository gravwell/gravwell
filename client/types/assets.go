package types

// AllowedOperations is the set of filter operators the registry accepts.
// Operators are also advertised per-field (and narrowed by type) via
// AvailableFilter.Operations; this list is the full union across all types.
var AllowedOperations = []string{"=", "!=", "<>", ">", "<", ">=", "<=", "LIKE", "NOT LIKE", "GLOB"}

// FilterType describes the value type expected by a filterable field, so a
// client can render an appropriate input control (text box, number box,
// toggle, date picker, user/group picker, ...).
type FilterType string

const (
	FilterTypeString FilterType = "string"
	FilterTypeInt    FilterType = "int"
	FilterTypeFloat  FilterType = "float"
	FilterTypeBool   FilterType = "bool"
	FilterTypeTime   FilterType = "time" // timestamp columns (CreatedAt, StartRange, ...)
	FilterTypeUID    FilterType = "uid"  // a user ID (OwnerID, LastModifiedByID)
	FilterTypeGID    FilterType = "gid"  // a group ID (Readers.GIDs, Writers.GIDs)
)

type QueryOptions struct {
	Type string `json:"type"` // Specifies the type of asset to return, 'mixed' for everything. Ignored except on the /api/list (ListAll) endpoint

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

// AvailableFilter describes a filter which *could* be applied to a field when
// listing assets. It carries everything a client needs to build a
// user-friendly "add a filter" menu without hardcoding per-field knowledge:
// the field key, a human label, the value type, the operators valid for that
// type, and optional hints (description, whether the field is sortable or holds
// multiple values).
type AvailableFilter struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Type        FilterType `json:"type"`
	Operations  []string   `json:"operations"`
	Sortable    bool       `json:"sortable"`
	MultiValued bool       `json:"multi_valued,omitempty"`
}
