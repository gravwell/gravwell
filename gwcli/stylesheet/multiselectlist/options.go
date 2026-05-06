/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package multiselectlist

type Options struct {
	// Returns a string to be prefixed to each item in the list to show it's selected state.
	//
	// Uses DefaultSelectedViewFunc if nil.
	ShowSelectStateFunc func(selected bool) string

	HideDescription bool
}
