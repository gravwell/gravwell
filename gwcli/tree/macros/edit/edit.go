/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package edit

import (
	"fmt"
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/connection"
	ft "github.com/gravwell/gravwell/v3/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold/scaffoldedit"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
)

const singular string = "macro"

func NewMacroEditAction() action.Pair {
	cfg := scaffoldedit.Config{
		"name": &scaffoldedit.Field{
			Required: true,
			Title:    "Name",
			Usage:    ft.Usage.Name(singular),
			FlagName: ft.Name.Name,
			Order:    100,
		},
		"description": &scaffoldedit.Field{
			Required: false,
			Title:    "Description",
			Usage:    ft.Usage.Desc(singular),
			FlagName: ft.Name.Desc,
			Order:    80,
		},
		"expansion": &scaffoldedit.Field{
			Required: true,
			Title:    "Expansion",
			Usage:    ft.Usage.Expansion,
			FlagName: ft.Name.Expansion,
			Order:    60,
		},
	}

	funcs := scaffoldedit.SubroutineSet[uint64, types.SearchMacro]{
		SelectSub: func(id uint64) (item types.SearchMacro, err error) {
			return connection.Client.GetMacro(id)
		},
		FetchSub: func() ([]types.SearchMacro, error) {
			return connection.Client.GetUserMacros(connection.MyInfo.UID)
		},
		GetFieldSub: func(item types.SearchMacro, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "expansion":
				return item.Expansion, nil
			}

			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.SearchMacro, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				if strings.Contains(val, " ") {
					return "name may not contain spaces", nil
				}
				val = strings.ToUpper(val)
				item.Name = val
			case "description":
				item.Description = val
			case "expansion":
				item.Expansion = val
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub: func(item types.SearchMacro) string {
			return fmt.Sprintf("%s -> %v", item.Name, item.Expansion)
		},
		GetDescriptionSub: func(item types.SearchMacro) string { return item.Description },
		UpdateSub: func(data *types.SearchMacro) (identifier string, err error) {
			if err := connection.Client.UpdateMacro(*data); err != nil {
				return "", err
			}
			return data.Name, nil
		},
	}

	return scaffoldedit.NewEditAction(singular, "macros", cfg, funcs)
}
