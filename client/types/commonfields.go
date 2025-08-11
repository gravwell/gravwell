package types

import (
	"time"
)

type AssetType string

const (
	AssetMacro AssetType = "macro"
)

type CommonFields struct {
	Type      AssetType
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt time.Time
	ID        string
	ParentID  string // the parent object this was cloned from

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
