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

// messageID returns m.MessageId, or "(unknown)" if it is nil. MessageId is a
// *string in the AWS SDK type, so it's not safe to dereference unconditionally
// when the value is only used for logging.
func messageID(m types.Message) string {
	if m.MessageId == nil {
		return "(unknown)"
	}
	return *m.MessageId
}

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
