/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package list

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/clilog"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/scaffold/scaffoldlist"

	"github.com/google/uuid"
	grav "github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

var (
	short          string   = "list extractors"
	long           string   = "list autoextractions available to you and the system"
	defaultColumns []string = []string{"UID", "UUID", "Name", "Desc"}
)

func NewExtractorsListAction() action.Pair {
	return scaffoldlist.NewListAction("", short, long, defaultColumns,
		types.AXDefinition{}, list, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.String("uuid", uuid.Nil.String(), "Fetches extraction by uuid.")
	return addtlFlags
}

func list(c *grav.Client, fs *pflag.FlagSet) ([]types.AXDefinition, error) {
	if id, err := fs.GetString("uuid"); err != nil {
		clilog.LogFlagFailedGet("uuid", err)
	} else {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if uid != uuid.Nil {
			clilog.Writer.Infof("Fetching ax with uuid %v", uid)
			d, err := c.GetExtraction(id)
			return []types.AXDefinition{d}, err
		}
		// if uid was nil, move on to normal get-all
	}

	return c.GetExtractions()
}
