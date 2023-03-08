//go:build !linux

// -build !linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package caps

type Capabilities uint64

const All Capabilities = 0xffffffffffffffff

func GetCaps() (Capabilities, error) {
	return All, nil
}

func Has(v Capabilities) bool {
	return true //just make sure nothing complains
}
