/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	crand "crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrUnderFill       = errors.New("short cryptographic buffer read")
	ErrBadTagCharacter = errors.New("Bad tag remap character")
)

const (
	defaultKeepAliveInterval = 2 * time.Second
)

// The implementation of this is actually in the Go stdlib, it's just not exported
// See math/rand/rand.go in the Go source tree.
type LockedSource struct {
	lk  sync.Mutex
	src rand.Source
}

func NewLockedSource(src rand.Source) rand.Source {
	return &LockedSource{src: src}
}

func (r *LockedSource) Int63() (n int64) {
	r.lk.Lock()
	n = r.src.Int63()
	r.lk.Unlock()
	return

}

func (r *LockedSource) Seed(seed int64) {
	r.lk.Lock()
	r.src.Seed(seed)
	r.lk.Unlock()
}

func NewRNG() (*rand.Rand, error) {
	seed, err := SecureSeed()
	if err != nil {
		return nil, err
	}
	return rand.New(NewLockedSource(rand.NewSource(seed))), nil
}

func NewInsecureRNG() *rand.Rand {
	return rand.New(NewLockedSource(rand.NewSource(rand.Int63())))
}

func SecureSeed() (int64, error) {
	bts := make([]byte, 8)
	if err := cfill(bts); err != nil {
		if err = cfill(bts); err != nil {
			if err = cfill(bts); err != nil {
				return -1, err
			}
		}
	}
	return int64(binary.LittleEndian.Uint64(bts)), nil
}

func cfill(v []byte) error {
	if n, err := crand.Read(v); err != nil {
		return err
	} else if n != len(v) {
		return ErrUnderFill
	}
	return nil
}

func isBadTagChar(r rune) bool {
	if !unicode.IsPrint(r) || unicode.IsControl(r) || unicode.IsSpace(r) {
		return true
	}

	//check specific restricted characters
	switch r {
	case '"', '\'', '`', 0xb4, 0x2018, 0x2019, 0x201c, 0x201d: //all the quote characters
		return true
	case '!', '*', ',', '^', '|', '$', '@', '\\', '/', '.', '<', '>', '{', '}', '[', ']':
		return true
	}
	return false
}

// CheckTag takes a tag name and returns an error if it contains any
// characters which are not allowed in tags.
func CheckTag(tag string) error {
	if tag = strings.TrimSpace(tag); len(tag) == 0 {
		return ErrEmptyTag
	} else if len(tag) > MAX_TAG_LENGTH {
		return ErrOversizedTag
	}
	for _, rn := range tag {
		if isBadTagChar(rn) {
			return ErrForbiddenTag
		}
	}
	return nil
}

// RemapTag takes a proposed tag string and remaps any forbidden characters to the provided character.
// err is set if the rchar is forbidden or the resulting tag is not valid.
func RemapTag(tag string, rchar rune) (rtag string, err error) {
	if isBadTagChar(rchar) {
		err = ErrBadTagCharacter
		return
	}
	if tag = strings.TrimSpace(tag); len(tag) == 0 {
		err = ErrEmptyTag
		return
	} else if len(tag) > MAX_TAG_LENGTH {
		err = ErrOversizedTag
		return
	}
	f := func(r rune) rune {
		if isBadTagChar(r) {
			return rchar
		}
		return r
	}
	rtag = strings.Map(f, tag)

	return
}

// EnableTCPKeepAlive enables TCP KeepAlive on the given connection,
// if it's a compatible connection type. If it is not, no action is
// taken.
func EnableKeepAlive(c net.Conn, period time.Duration) {
	if c == nil {
		return //ok...
	}
	if period <= 0 {
		period = defaultKeepAliveInterval
	}
	switch v := c.(type) {
	case *net.TCPConn:
		v.SetKeepAlive(true)
		v.SetKeepAlivePeriod(period)
	case *tls.Conn:
		nc := v.NetConn()
		if tc, ok := nc.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(period)
		}
	}
}
