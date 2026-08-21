package types

import "encoding/json"

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

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (q QueryOptions) MarshalJSON() ([]byte, error) {
	type dummyQueryOptions QueryOptions
	q.Filters = nonNilSlice(q.Filters)
	return json.Marshal(dummyQueryOptions(q))
}

// Filter based on the values given, e.g. Key = "Name", Operation = "=", Values = ["foo", "bar"].
// Specifying multiple values is an implicit OR.
type Filter struct {
	Key       string
	Operation string
	Values    []any
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (f Filter) MarshalJSON() ([]byte, error) {
	type dummyFilter Filter
	f.Values = nonNilSlice(f.Values)
	return json.Marshal(dummyFilter(f))
}

// AvailableFilter describes a filter which *could* be applied to a field when
// listing assets. It carries everything a client needs to build a
// user-friendly "add a filter" menu without hardcoding per-field knowledge:
// the field key, a human label, the value type, the operators valid for that
// type, and optional hints (description, whether the field is sortable or holds
// multiple values).
type AvailableFilter struct {
	Key         string
	Label       string
	Description string `json:",omitempty"`
	Type        FilterType
	Operations  []string
	Sortable    bool
	MultiValued bool `json:",omitempty"`
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (a AvailableFilter) MarshalJSON() ([]byte, error) {
	type dummyAvailableFilter AvailableFilter
	a.Operations = nonNilSlice(a.Operations)
	return json.Marshal(dummyAvailableFilter(a))
}
