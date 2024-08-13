/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package create

import (
	"gwcli/action"
	"gwcli/connection"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold/scaffoldcreate"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

func NewMacroCreateAction() action.Pair {
	n := scaffoldcreate.NewField(true, "name", 100)
	n.FlagShorthand = 'n'
	d := scaffoldcreate.NewField(true, "description", 90)
	d.FlagShorthand = 'd'

	fields := scaffoldcreate.Config{
		"name": n,
		"desc": d,
		"exp": scaffoldcreate.Field{
			Required:      true,
			Title:         "expansion",
			Usage:         ft.Usage.Expansion,
			Type:          scaffoldcreate.Text,
			FlagName:      ft.Name.Expansion,
			FlagShorthand: 'e',
			DefaultValue:  "",
			Order:         80,
		},
	}

	return scaffoldcreate.NewCreateAction("macro", fields, create, nil)
}

func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values, _ *pflag.FlagSet) (any, string, error) {
	sm := types.SearchMacro{}
	// all three fields are required, no need to nil-check them
	sm.Name = strings.ToUpper(vals["name"])
	sm.Description = vals["desc"]
	sm.Expansion = vals["exp"]

	id, err := connection.Client.AddMacro(sm)
	return id, "", err

}
