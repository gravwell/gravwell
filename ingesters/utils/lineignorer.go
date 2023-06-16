/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"bytes"
	"unsafe"

	"github.com/gobwas/glob"
)

type LineIgnorer struct {
	prefixes [][]byte
	globs    []glob.Glob
}

func NewIgnorer(prefixes, globs []string) (*LineIgnorer, error) {
	li := &LineIgnorer{}
	for _, v := range prefixes {
		if len(v) > 0 {
			li.prefixes = append(li.prefixes, []byte(v))
		}
	}
	for _, v := range globs {
		c, err := glob.Compile(v)
		if err != nil {
			return nil, err
		}
		li.globs = append(li.globs, c)
	}
	return li, nil
}

// Ignore returns true if the given byte slice matches any of the prefixes or
// globs in the ignorer.
func (l *LineIgnorer) Ignore(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, prefix := range l.prefixes {
		if bytes.HasPrefix(b, prefix) {
			return true
		}
	}

	bString := unsafe.String(&b[0], len(b))
	for _, glob := range l.globs {
		if glob.Match(bString) {
			return true
		}
	}

	return false
}
