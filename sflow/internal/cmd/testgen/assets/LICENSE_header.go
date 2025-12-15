/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package assets
package assets

import (
	_ "embed"
	"fmt"
	"time"
)

//go:embed LICENSE_header.txt
var rawLICENSEHeader []byte

var LICENSEHeader = []byte(fmt.Sprintf(string(rawLICENSEHeader), time.Now().Year()))
