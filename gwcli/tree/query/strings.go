/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

/*
Collection of pre-formatted strings to ensure consistency between the interactive and non-interactive variants.
*/

// The given search associated to SID was submitted in the background and our job is done.
func BackgroundedQuerySuccess(sid string) string {
	return ("Successfully backgrounded query (ID: " + sid + ")")
}
