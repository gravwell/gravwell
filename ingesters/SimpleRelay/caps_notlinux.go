//go:build !linux

// -build !linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"github.com/gravwell/gravwell/v3/ingest/log"
)

func capsOk(lg *log.Logger) bool {
	return true
}
