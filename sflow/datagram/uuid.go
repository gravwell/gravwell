/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"strings"
)

type SFlowUUID [16]byte

func (sui SFlowUUID) String() string {
	var sb strings.Builder

	sb.WriteString(string(sui[:4]))
	sb.WriteByte('-')
	sb.WriteString(string(sui[4:8]))
	sb.WriteByte('-')
	sb.WriteString(string(sui[8:12]))
	sb.WriteByte('-')
	sb.WriteString(string(sui[12:16]))

	return sb.String()
}
