/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"fmt"

	"github.com/crewjam/rfc5424"
)

func KV(name string, value interface{}) (r rfc5424.SDParam) {
	r.Name = name
	switch v := value.(type) {
	case string:
		r.Value = v
	default:
		r.Value = fmt.Sprintf("%v", value)
	}
	return
}

func KVErr(err error) rfc5424.SDParam {
	return KV("error", err)
}
