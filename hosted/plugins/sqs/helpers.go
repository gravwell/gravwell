/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs

import (
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func ExtractTimestamp(m types.Message, ignoreTimestamps bool) time.Time {
	if ignoreTimestamps {
		return time.Now()
	}
	v, ok := m.Attributes["SentTimestamp"]
	if !ok {
		return time.Now()
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.UnixMilli(ms)
}
