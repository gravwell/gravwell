/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package listitem defines common list types so we don't have a bunch of duplicate structs floating around any time list.Model or
// multiselectlist.Model are used.
package listitem

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
)

//#region User

type User struct {
	Selected_ bool

	U types.User

	DescriptionOverride string // if set, will be used in place of the default description.
}

// NewUserItem returns a representation of the given user prepared for use in a list.Model or a multiselectlist.Model.
func NewUserItem(u types.User, selected bool) *User {
	return &User{
		Selected_: selected,
		U:         u,
	}
}

var _ multiselectlist.SelectableItem[int32] = &User{}
var _ list.Item = &User{}

// FilterValue filters on the concat of ttl and desc.
func (li User) FilterValue() string {
	var adm string
	if li.U.Admin {
		adm = "admin"
	}
	return adm + fmt.Sprintf("%d %v %v", li.U.ID, li.U.Username, li.U.Name)
}

func (li User) Title() string {
	return fmt.Sprintf("(%d) %s", li.U.ID, li.U.Username)
}

func (li User) ID() int32 {
	return li.U.ID
}

func (li User) Description() string {
	if li.DescriptionOverride != "" {
		return li.DescriptionOverride
	}

	var sb strings.Builder

	if li.U.Admin {
		sb.WriteString("(admin) ")
	}
	fmt.Fprintf(&sb, "%s (%s)", li.U.Name, li.U.Email)

	return sb.String()
}

func (li *User) SetSelected(selected bool) {
	li.Selected_ = selected
}

func (li User) Selected() bool {
	return li.Selected_
}

//#region Group

type Group struct {
	Selected_ bool

	G types.Group

	DescriptionOverride string // if set, will be used in place of the default description.
}

// NewGroupItem returns a representation of the given group prepared for use in a list.Model or a multiselectlist.Model.
func NewGroupItem(g types.Group, selected bool) *Group {
	return &Group{
		Selected_: selected,
		G:         g,
	}
}

var _ multiselectlist.SelectableItem[int32] = &Group{}
var _ list.Item = &Group{}

// FilterValue filters on the concat of ttl and desc.
func (li Group) FilterValue() string {
	return fmt.Sprintf("%d %s %s", li.G.ID, li.G.Name, li.G.Description)
}

func (li Group) Title() string {
	return fmt.Sprintf("(%d) %s", li.G.ID, li.G.Name)
}

func (li Group) ID() int32 {
	return li.G.ID
}

func (li Group) Description() string {
	if li.DescriptionOverride != "" {
		return li.DescriptionOverride
	}

	return li.G.Description
}

func (li *Group) SetSelected(selected bool) {
	li.Selected_ = selected
}

func (li Group) Selected() bool {
	return li.Selected_
}
