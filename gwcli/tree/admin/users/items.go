package admin_users

import (
	"fmt"
	"strings"
)

type userItem struct {
	Selected_ bool
	ID_       int32

	username string
	name     string
	email    string
	admin    bool
}

// FilterValue filters on the concat of ttl and desc.
func (i userItem) FilterValue() string {
	var adm string
	if i.admin {
		adm = "admin"
	}
	return adm + fmt.Sprintf("%d %v %v", i.ID_, i.username, i.name)
}

func (i userItem) Title() string {
	return i.username
}

func (i userItem) ID() int32 {
	return i.ID_
}

func (i userItem) Description() string {
	var sb strings.Builder

	if i.admin {
		sb.WriteString("(admin) ")
	}
	fmt.Fprintf(&sb, "(ID: %d) %s (%s)", i.ID_, i.name, i.email)

	return sb.String()
}

func (i *userItem) SetSelected(selected bool) {
	i.Selected_ = selected
}

func (i userItem) Selected() bool {
	return i.Selected_
}
