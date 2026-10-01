/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Options struct {
	scaffold.CommonOptions
	// Called as soon as the action is invoked, before the providers' SetArgs hooks and before fields are set from flags.
	// You may assume that the flags have already been parsed, but that no additional actions have been taken on them.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
	// If set, the any "id" returned from CreateFunc will be printed bare, rather than being fed into phrases.SuccessfullyCreatedItem.
	IDIsSuccessMessage bool
}

// Apply alters the given cmd such that all set Options are effectual.
func (o Options) Apply(cmd *cobra.Command) {
	o.CommonOptions.Apply(cmd) // call super
	if o.Short != "" {
		cmd.Short = o.Short
	}
	if o.Long != "" {
		cmd.Long = o.Long
	}
}
