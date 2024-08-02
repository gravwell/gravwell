/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/**
 * gwcli is driven by mother/root.go's .Execute() method, which is called here
 */

package main

import (
	"gwcli/tree"
	"os"
)

func init() {
}

func main() {
	os.Exit(tree.Execute(nil))
}
