/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ft

import "fmt"

// This file provides standardized text for invalid arguments.

// InvAtMostArgN returns text stating that at most N bare arguments are allowed.
func InvAtMostArgN(atMost, given uint) (invalid string) {
	argStr := "argument"
	if atMost != 1 {
		argStr += "s"
	}
	return fmt.Sprintf("at most %d %s may be specified (%d given)", atMost, argStr, given)
}
