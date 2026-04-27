package admin_users

import (
	"fmt"
	"strings"
)

// DefaultSelectableItem provides a struct for use in the SelectableItem interface for users that do not wish to customize at all.
type UserItem struct {
	Selected_ bool
	ID_       int32

	username string
	name     string
	email    string
	admin    bool
}

// FilterValue filters on the concat of ttl and desc.
func (i UserItem) FilterValue() string {
	var adm string
	if i.admin {
		adm = "admin"
	}
	return adm + fmt.Sprintf("%d %v %v", i.ID_, i.username, i.name)
}

func (i UserItem) Title() string {
	return i.username
}

func (i UserItem) ID() int32 {
	return i.ID_
}

func (i UserItem) Description() string {
	var sb strings.Builder

	if i.admin {
		sb.WriteString("(admin) ")
	}
	fmt.Fprintf(&sb, "(ID: %d) %s (%s)", i.ID_, i.name, i.email)

	return sb.String()
}

func (i *UserItem) SetSelected(selected bool) {
	i.Selected_ = selected
}

func (i UserItem) Selected() bool {
	return i.Selected_
}
