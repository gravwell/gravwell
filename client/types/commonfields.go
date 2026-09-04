package types

import (
	"slices"
	"time"
)

type AssetType string

const (
	AssetMacro                  AssetType = "macro"
	AssetToken                  AssetType = "token"
	AssetAX                     AssetType = "ax"
	AssetSavedQuery             AssetType = "saved_query"
	AssetResource               AssetType = "resource"
	AssetFile                   AssetType = "file"
	AssetTemplate               AssetType = "template"
	AssetSearchHistory          AssetType = "search_history"
	AssetUserPreference         AssetType = "user_preference"
	AssetSecret                 AssetType = "secret"
	AssetScheduledSearch        AssetType = "scheduled_search"
	AssetScheduledScript        AssetType = "scheduled_script"
	AssetFlow                   AssetType = "flow"
	AssetScheduledSearchResults AssetType = "scheduled_search_results"
	AssetScheduledScriptResults AssetType = "scheduled_script_results"
	AssetFlowResults            AssetType = "flow_results"
	AssetAlert                  AssetType = "alert"
	AssetPlaybook               AssetType = "playbook"
	AssetDashboard              AssetType = "dashboard"
	AssetActionable             AssetType = "actionable"
	AssetKitBuildRequest        AssetType = "kit_build_request"
	AssetSearchInfo             AssetType = "search_info"
)

// AssetTypeMap maps the string value of each AssetType constant to its AssetType value.
var AssetTypeMap = map[string]AssetType{
	string(AssetMacro):                  AssetMacro,
	string(AssetToken):                  AssetToken,
	string(AssetAX):                     AssetAX,
	string(AssetSavedQuery):             AssetSavedQuery,
	string(AssetResource):               AssetResource,
	string(AssetFile):                   AssetFile,
	string(AssetTemplate):               AssetTemplate,
	string(AssetSearchHistory):          AssetSearchHistory,
	string(AssetUserPreference):         AssetUserPreference,
	string(AssetSecret):                 AssetSecret,
	string(AssetScheduledSearch):        AssetScheduledSearch,
	string(AssetScheduledScript):        AssetScheduledScript,
	string(AssetFlow):                   AssetFlow,
	string(AssetScheduledSearchResults): AssetScheduledSearchResults,
	string(AssetScheduledScriptResults): AssetScheduledScriptResults,
	string(AssetFlowResults):            AssetFlowResults,
	string(AssetAlert):                  AssetAlert,
	string(AssetPlaybook):               AssetPlaybook,
	string(AssetDashboard):              AssetDashboard,
	string(AssetActionable):             AssetActionable,
	string(AssetKitBuildRequest):        AssetKitBuildRequest,
	string(AssetSearchInfo):             AssetSearchInfo,
}

// ValidateAssetType returns true if s corresponds to a known AssetType.
func ValidateAssetType(s string) bool {
	_, ok := AssetTypeMap[s]
	return ok
}

type CommonFields struct {
	Type      AssetType
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt time.Time
	ID        string
	// the parent object this was cloned from.
	// Not user settable.
	ParentID string

	OwnerID int32
	Owner   User

	// Permissions
	Readers ACL
	Writers ACL

	// Tracks who made the last change to this item
	LastModifiedByID int32
	LastModifiedBy   User

	Name        string
	Description string
	Labels      []string
	Version     int

	// If set, associates the asset with a kit of the specified ID
	Kit string

	// Auto-generated for the requesting user based on permissions of this object.
	Can Actions
}

func (cf *CommonFields) CanRead(u *User) bool {
	// Owner and admins can always read
	if u.ID == cf.OwnerID || u.Admin {
		return true
	}
	if cf.Readers.Global {
		return true
	}
	// Check allowed groups
	for i := range cf.Readers.GIDs {
		for j := range u.Groups {
			if cf.Readers.GIDs[i] == u.Groups[j].ID {
				return true
			}
		}
	}
	// If all else fails, anyone who can read is allowed to write too.
	return cf.CanWrite(u)
}

func (cf *CommonFields) CanWrite(u *User) bool {
	// Owner and admins can always write
	if u.ID == cf.OwnerID || u.Admin {
		return true
	}
	if cf.Writers.Global {
		return true
	}
	// Check allowed groups
	for i := range cf.Writers.GIDs {
		for j := range u.Groups {
			if cf.Writers.GIDs[i] == u.Groups[j].ID {
				return true
			}
		}
	}
	return false
}

func (cf *CommonFields) GroupCanRead(gid int32) bool {
	for i := range cf.Readers.GIDs {
		if cf.Readers.GIDs[i] == gid {
			return true
		}
	}
	return cf.GroupCanWrite(gid)
}

func (cf *CommonFields) GroupCanWrite(gid int32) bool {
	for i := range cf.Writers.GIDs {
		if cf.Writers.GIDs[i] == gid {
			return true
		}
	}
	return false
}

// AllGIDs returns the union of Readers.GIDs and Writers.GIDs, i.e. every
// group that has any access (read and/or write) to this asset. Readers.GIDs
// is assumed to already be deduplicated.
func (cf *CommonFields) AllGIDs() []int32 {
	gids := append([]int32(nil), cf.Readers.GIDs...)
	for _, g := range cf.Writers.GIDs {
		if !slices.Contains(gids, g) {
			gids = append(gids, g)
		}
	}
	return gids
}

// CommonFieldsPatch is the base type used to request updates to existing assets.
// It contains only fields that can be updated.
type CommonFieldsPatch struct {
	Description Optional[string]   `json:",omitzero"`
	Labels      Optional[[]string] `json:",omitzero"`
	Name        Optional[string]   `json:",omitzero"`
	OwnerID     Optional[int32]    `json:",omitzero"`
	Readers     Optional[ACL]      `json:",omitzero"`
	Writers     Optional[ACL]      `json:",omitzero"`
}

// ToPatch converts cf into a CommonFieldsPatch with every field set.
func (cf CommonFields) ToPatch() CommonFieldsPatch {
	return CommonFieldsPatch{
		Description: NewOptional(cf.Description),
		Labels:      NewOptional(cf.Labels),
		Name:        NewOptional(cf.Name),
		OwnerID:     NewOptional(cf.OwnerID),
		Readers:     NewOptional(cf.Readers),
		Writers:     NewOptional(cf.Writers),
	}
}

type ListAllResponse struct {
	BaseListResponse
	Results []CommonFields
}
