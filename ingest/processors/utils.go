/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"github.com/inhies/go-bytesize"
)

func parseDataSize(v string) (s int, err error) {
	var bs bytesize.ByteSize
	if bs, err = bytesize.Parse(v); err == nil {
		s = int(bs)
	}
	return
}
