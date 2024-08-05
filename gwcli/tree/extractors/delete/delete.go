/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package delete

import (
	"errors"
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	"gwcli/utilities/scaffold/scaffolddelete"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/client/types"
)

func NewExtractorDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("extractor", "extractors",
		del, fetch)
}

func del(dryrun bool, id uuid.UUID) error {
	if dryrun {
		_, err := connection.Client.GetExtraction(id.String())
		return err
	}
	if wrs, err := connection.Client.DeleteExtraction(id.String()); err != nil {
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

func fetch() ([]scaffolddelete.Item[uuid.UUID], error) {
	axs, err := connection.Client.GetExtractions()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(axs, func(a1, a2 types.AXDefinition) int {
		return strings.Compare(a1.Name, a2.Name)
	})
	var items = make([]scaffolddelete.Item[uuid.UUID], len(axs))
	for i, ax := range axs {
		items[i] = scaffolddelete.NewItem[uuid.UUID](ax.Name,
			fmt.Sprintf("module: %v\ntags: %v\n%v",
				stylesheet.Header2Style.Render(ax.Module),
				stylesheet.Header2Style.Render(strings.Join(ax.Tags, " ")),
				ax.Desc),
			ax.UUID)
	}

	return items, nil
}
