/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package extractors

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
)

func newExtractorDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("extractor", "extractors",
		del, fetch)
}

func del(dryrun bool, id string) error {
	if dryrun {
		_, err := connection.Client.GetExtraction(id)
		return err
	}
	if wrs, err := connection.Client.DeleteExtraction(id); err != nil {
		return err
	} else if wrs != nil {
		var sb strings.Builder
		sb.WriteString("failed to delete ax with warning(s):")
		for _, wr := range wrs {
			sb.WriteString("\n" + wr.Err.Error())
		}
		clilog.Writer.Warn(sb.String())
		return errors.New(sb.String())
	}
	return nil
}

func fetch() ([]scaffolddelete.Item[string], error) {
	axl, err := connection.Client.ListExtractions(nil)
	if err != nil {
		return nil, err
	}
	axs := axl.Results
	slices.SortFunc(axs, func(a1, a2 types.AX) int {
		return strings.Compare(a1.Name, a2.Name)
	})
	var items = make([]scaffolddelete.Item[string], len(axs))
	for i, ax := range axs {
		items[i] = scaffolddelete.NewItem[string](ax.Name,
			fmt.Sprintf("module: %v\ntags: %v\n%v",
				stylesheet.Cur.SecondaryText.Render(ax.Module),
				stylesheet.Cur.SecondaryText.Render(strings.Join(ax.Tags, " ")),
				ax.Description),
			ax.ID)
	}

	return items, nil
}
