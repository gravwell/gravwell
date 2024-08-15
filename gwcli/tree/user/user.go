/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package user

import (
	"github.com/gravwell/gravwell/v3/gwcli/action"
	"github.com/gravwell/gravwell/v3/gwcli/tree/user/admin"
	"github.com/gravwell/gravwell/v3/gwcli/tree/user/logout"
	"github.com/gravwell/gravwell/v3/gwcli/tree/user/myinfo"
	"github.com/gravwell/gravwell/v3/gwcli/tree/user/refreshmyinfo"
	"github.com/gravwell/gravwell/v3/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

const (
	use   string = "user"
	short string = "manage your user and profile"
	long  string = "View and edit properties of your current, logged in user."
)

var aliases []string = []string{"self"}

func NewUserNav() *cobra.Command {
	return treeutils.GenerateNav(use, short, long, aliases, nil,
		[]action.Pair{logout.NewUserLogoutAction(),
			admin.NewUserAdminAction(),
			myinfo.NewUserMyInfoAction(),
			refreshmyinfo.NewUserRefreshMyInfoAction()})
}
