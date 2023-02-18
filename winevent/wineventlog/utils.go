/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wineventlog

import (
	"io"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// UTF16LEBufferToUTF8Bytes takes UTF-16 in little endian encoding without a BOM and spits it back
// out as UTF8.  Basically take the insanity of Windows native strings and turn it back
// into nice clean UTF-8, just like the way mom used to make it.
func UTF16LEBufferToUTF8Bytes(v []byte) (r []byte, err error) {
	r, _, err = transform.Bytes(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder(), v)
	return
}

func UTF16LEBufferToUTF8Writer(v []byte, out io.Writer) (err error) {
	var bts []byte
	if bts, err = UTF16LEBufferToUTF8Bytes(v); err == nil {
		_, err = out.Write(bts)
	}
	return
}

// UTF16LEToUTF8 wraps UTF16LEToUTF8Bytes to return a string
func UTF16LEToUTF8(v []byte) (s string, err error) {
	if buff, lerr := UTF16LEBufferToUTF8Bytes(v); lerr == nil {
		s = string(buff)
	}
	return
}

func UTF16LEToUTF8Bytes(v []uint16) (r []byte) {
	runes := utf16.Decode(v)
	for _, ru := range runes {
		if utf8.ValidRune(ru) {
			r = utf8.AppendRune(r, ru)
		}
	}
	return
}
