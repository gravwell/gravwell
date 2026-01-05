/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"strings"
)

// SFlowUUID is a fixed 16-byte UUID per RFC 4122.
// The sFlow spec (https://sflow.org/sflow_host.txt) notates this as "opaque uuid<16>"
// but both host-sflow and sflowtool treat it as a fixed array with no length prefix.
// See:
//
// - https://github.com/sflow/host-sflow/blob/master/src/AIX/readHidCounters.c#L43
//
// - https://github.com/sflow/sflowtool/blob/master/src/sflowtool.c#L4586
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
