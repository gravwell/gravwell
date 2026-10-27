package types

import (
	"fmt"
)

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
	Type string // Specifies the type of asset to return, 'mixed' for everything. Ignored except on the /api/list/assets (ListAll) endpoint

	IncludeDeleted bool
	Version        int32 // fetch a particular version, when appropriate. 0 means latest, -1 means all versions (list only)

	// All requests items owned by every user, not just the caller.
	// It is silently ignored if the caller is not an admin.
	All bool

	// Listing options
	OrderBy        string `json:",omitzero"` // Sort by this field
	OrderDirection string `json:",omitzero"`
	CursorID       string // Return assets whose ID is greater than the given ID.
	// Max number of assets to return.
	// Capped by QueryOptionsMaxLimit, defaults to QueryOptionsDefaultLimit.
	Limit int32 `json:",omitzero"`

	// Filtering on fields of assets
	Filters []Filter

	// Search filters on multiple fields for a rough string match
	Search string
}

const (
	QueryOptionsDefaultLimit = 100
	QueryOptionsMaxLimit     = 1000
	QueryOptionsMaxVersion   = 2147483647
)

var ErrBadQueryOptionsLimit = fmt.Errorf("limit must be 0 < x <= %d", QueryOptionsMaxLimit)
var ErrBadQueryOptionsVersion = fmt.Errorf("version must be -1 <= x <= %d", QueryOptionsMaxVersion)

// DefaultQueryOptions returns a QueryOptions with the default values as expected by the api spec.
// An empty request against a list endpoint will default to these values.
func DefaultQueryOptions() QueryOptions {
	return QueryOptions{
		Limit: QueryOptionsDefaultLimit,
	}
}

// Validate confirms that qo fits the api spec.
func (qo QueryOptions) Validate() error {
	if qo.Limit < 1 || qo.Limit > QueryOptionsMaxLimit {
		return ErrBadQueryOptionsLimit
	}
	if qo.Version < -1 {
		return ErrBadQueryOptionsVersion
	}
	return nil
}

// Filter based on the values given, e.g. Key = "Name", Operation = "=", Values = ["foo", "bar"].
// Specifying multiple values is an implicit OR.
type Filter struct {
	Key       string
	Operation string
	Values    []any
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
