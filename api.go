/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"fmt"
	"io"
)

const (
	//MAJOR API VERSIONS should always be compatible, there just may be additional features
	API_VERSION_MAJOR uint32 = 0
	API_VERSION_MINOR uint32 = 4
)

func PrintVersion(wtr io.Writer) {
	fmt.Fprintf(wtr, "API Version:\t%d.%d\n", API_VERSION_MAJOR, API_VERSION_MINOR)
}

type Logger interface {
	Info(string, ...interface{}) error
	Warn(string, ...interface{}) error
	Error(string, ...interface{}) error
}
