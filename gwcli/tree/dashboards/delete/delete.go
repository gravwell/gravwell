/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package delete

import (
	"fmt"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"time"
)

func NewDashboardDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("dashboard", "dashboards",
		del, fch)
}

func del(dryrun bool, id uint64) error {
	if dryrun {
		_, err := connection.Client.GetDashboard(id)
		return err
	}
	return connection.Client.DeleteDashboard(id)
}

func fch() ([]scaffolddelete.Item[uint64], error) {
	ud, err := connection.Client.GetUserDashboards(connection.MyInfo.UID)
	if err != nil {
		return nil, err
	}
	// not too important to sort this one
	var items = make([]scaffolddelete.Item[uint64], len(ud))
	for i, u := range ud {
		items[i] = scaffolddelete.NewItem(u.Name,
			fmt.Sprintf("Updated: %v\n%s",
				ud[i].Updated.Format(time.RFC822), ud[i].Description),
			u.ID)
	}

	return items, nil
}
