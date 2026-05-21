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
)

type Options struct {
	scaffold.CommonOptions
	// Override scaffoldcreate's default one-line action description.
	Short string
	// Override scaffoldcreate's default action description.
	Long string
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
