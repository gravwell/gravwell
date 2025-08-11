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
