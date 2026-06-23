/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package listitem defines common list types so we don't have a bunch of duplicate structs floating around any time list.Model or
// multiselectlist.Model are used.
// Some Wrap functions are provided so MSLs of a given type look and operate comparably between actions.
package listitem

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// Generic provides a general-purpose list item for types that do not require much special handling to be stuffed into a list.Model or MSL.
type Generic struct {
	Selected_  bool
	ID_        string
	Name       string
	SecondLine string // whatever you want the description to be

	ShowDisabled bool // if set, "(disabled)" will be prefixed to second line if !Enabled.
	Enabled      bool
}

// FilterValue filters on the concat of ttl and desc.
func (li Generic) FilterValue() string {
	return fmt.Sprintf("%s %v %v", li.ID_, li.Name, li.SecondLine)
}

// Title displays the item ID and name.
func (li Generic) Title() string {
	return fmt.Sprintf("(%s) %s", li.ID_, li.Name)
}

func (li Generic) ID() string {
	return li.ID_
}

// Description displays SecondLine, optionally prefixed by "(disabled)" if ShowDisabled && !Enabled.
func (li Generic) Description() string {
	prefix := "(disabled)"
	if li.ShowDisabled && !li.Enabled {
		return prefix + li.SecondLine
	}
	return li.SecondLine
}

func (li *Generic) SetSelected(selected bool) {
	li.Selected_ = selected
}

func (li Generic) Selected() bool {
	return li.Selected_
}

// GetGeneric asserts that the currently selected item is a Generic item and returns it as such.
func GetGeneric(l *list.Model) (*Generic, error) {
	u, ok := l.SelectedItem().(*Generic)
	if !ok {
		return &Generic{}, clilog.TypeAssert(l.SelectedItem(), &Generic{})
	}
	return u, nil
}

//#region User

// User provides a list item for types.User.
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

// GetUser returns the currently selected group from a given list.
//
// Returns ErrInternal and logs the error if something goes wrong.
func GetUser(l *list.Model) (types.User, error) {
	u, ok := l.SelectedItem().(*User)
	if !ok {
		return types.User{}, clilog.TypeAssert(l.SelectedItem(), &User{})
	}
	return u.U, nil
}

//#region Group

// Group provides a list item for types.Group.
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

// GetGroup returns the currently selected group from a given list.
//
// Returns ErrInternal and logs the error if something goes wrong.
func GetGroup(l *list.Model) (types.Group, error) {
	itm := l.SelectedItem()
	if itm == nil {
		clilog.Writer.Error("selected item is nil")
		return types.Group{}, clilog.ErrInternal{}
	}
	g, ok := itm.(*Group)
	if !ok {
		return types.Group{}, clilog.TypeAssert(l.SelectedItem(), &Group{})
	}
	return g.G, nil
}

// WrapDashboards returns an MSL and list.Model ready array of the given dashboards.
// No items are marked as selected.
func WrapDashboards(x []types.Dashboard) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: itm.Description,
		}
	}
	return items
}

// WrapTemplates returns an MSL and list.Model ready array of the given templates.
// No items are marked as selected.
func WrapTemplates(x []types.Template) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: itm.Description,
		}
	}
	return items
}

// WrapActionables returns an MSL and list.Model ready array of the given actionables.
// No items are marked as selected.
func WrapActionables(x []types.Actionable) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_:    false,
			ID_:          itm.ID,
			Name:         itm.Name,
			SecondLine:   itm.Description,
			ShowDisabled: true,
			Enabled:      !itm.Disabled,
		}
	}
	return items
}

// WrapFlows returns an MSL and list.Model ready array of the given flows.
// No items are marked as selected.
func WrapFlows(x []types.Flow) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			ID_:          itm.ID,
			Name:         itm.Name,
			SecondLine:   fmt.Sprintf("[%s] %s", itm.Schedule, itm.Description),
			ShowDisabled: true,
			Enabled:      !itm.Disabled,
		}
	}
	return items
}

// WrapScheduledSearches returns an MSL and list.Model ready array of the given scheduled searches.
// No items are marked as selected.
func WrapScheduledSearches(x []types.ScheduledSearch) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		line := fmt.Sprintf("[%s] %s", itm.Schedule, itm.SearchString)
		if itm.Description != "" {
			line += " - " + itm.Description
		}
		items[i] = &Generic{
			ID_:          itm.ID,
			Name:         itm.Name,
			SecondLine:   line,
			ShowDisabled: true,
			Enabled:      !itm.Disabled,
		}
	}
	return items
}

// WrapResources returns an MSL and list.Model ready array of the given resources.
// No items are marked as selected.
func WrapResources(x []types.Resource) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_:  false,
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: fmt.Sprintf("(Size: %v) %s", itm.Size, itm.Description),
		}
	}
	return items
}

// WrapMacros returns an MSL and list.Model ready array of the given macros.
// No items are marked as selected.
func WrapMacros(x []types.Macro) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_:  false,
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: itm.Description,
		}
	}
	return items
}

// WrapAXs returns an MSL and list.Model ready array of the given extractors.
// No items are marked as selected.
func WrapAXs(x []types.AX) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_: false,
			ID_:       itm.ID,
			Name:      itm.Name,
			SecondLine: fmt.Sprintf("%s/%s|%s", stylesheet.Cur.SecondaryText.Render(itm.Module),
				stylesheet.Cur.SecondaryText.Render("["+strings.Join(itm.Tags, " ")+"]"),
				itm.Description),
		}
	}
	return items
}

// WrapFiles returns an MSL and list.Model ready array of the given files.
// No items are marked as selected.
func WrapFiles(x []types.File) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: fmt.Sprintf("(Size: %v) %s", itm.Size, itm.Description),
		}
	}
	return items
}

// WrapPlaybooks returns an MSL and list.Model ready array of the given playbooks.
// No items are marked as selected.
func WrapPlaybooks(x []types.Playbook) []multiselectlist.SelectableItem[string] {
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_:  false,
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: itm.Description,
		}
	}
	return items
}

// WrapAlerts returns an MSL and list.Model ready array of the given alerts.
// No items are marked as selected.
func WrapAlerts(x []types.Alert) []multiselectlist.SelectableItem[string] {
	// sort on name
	slices.SortStableFunc(x, func(a, b types.Alert) int { return strings.Compare(a.Name, b.Name) })
	items := make([]multiselectlist.SelectableItem[string], len(x))
	for i, itm := range x {
		items[i] = &Generic{
			Selected_:  false,
			ID_:        itm.ID,
			Name:       itm.Name,
			SecondLine: itm.Description,

			ShowDisabled: true,
			Enabled:      !itm.Disabled,
		}
	}
	return items
}
