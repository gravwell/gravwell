/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"math/rand"
	"sync"
)

var (
	ErrUnderFill = errors.New("short cryptographic buffer read")
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
