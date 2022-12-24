/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	evblockHeaderLen = 8
	MaxEvBlockCount  = 0xffff           //bonkers, but we need to be safe
	MaxEvBlockSize   = 1024 * 1024 * 32 //not too lean, but let's remove opportunity for crazy memory pressure

)

var (
	ErrEnumeratedValueBlockInvalidCount = errors.New("enumerated value block has too many evs")
	ErrEnumeratedValueBlockInvalidSize  = errors.New("enumerated value block is too large")
	ErrEnumeratedValueBlockCorrupt      = errors.New("enumerated value block buffer is corrupted")
)

// evblockheader type expressed for documentation, defines transport header for evlocks
type evblockheader struct {
	size  uint32 //this is the complete size, including the header
	count uint16
	delim uint16
}

// evblock is a block of enumerated values, this is used to help with transporting enumerated values
// over the wire and to guard against users of the API doing wonky things that become expensive
// to encode/decode
type evblock struct {
	// keep the size running as we go so encoding is less expensive to ask for it
	// size is the encoded size, it includes the header, we keep this so its easy
	// to pre-allocate buffers when needed
	size uint64
	evs  []EnumeratedValue
}

// Add adds an enumerated value to an evbloc, this function keeps a running tally of size for fast query
func (eb *evblock) Add(ev EnumeratedValue) {
	if eb.size == 0 {
		eb.size = evblockHeaderLen
	}
	eb.size += uint64(ev.Size())
	eb.evs = append(eb.evs, ev)
}

// Size is just a helper accessor to help with encoding efficiency
func (eb evblock) Size() uint64 {
	return eb.size
}

// Populated is a helper to check if there are any EVs
func (eb evblock) Populated() bool {
	return eb.size > 0
}

// Values is an accessor to actualy get the set of enumerated values out of the evblock
// this returns the slice directly, so callers COULD mess with the slice and break the size
// tracker.  Basically don't re-use or assign to this slice, if you do the evblock you pulled it from
// is no longer valid
func (eb evblock) Values() []EnumeratedValue {
	if len(eb.evs) > 0 {
		return eb.evs
	}
	return nil
}

// Valid is a helper to determine if an evblock is valid for transport
// this means that the max ev count hasn't been exceeded nor has the max size.
// If an evblock is empty, it IS valid.  So transports should check Populated
// in addition to valid, when deciding which Entry encoder to use.
func (eb evblock) Valid() error {
	if len(eb.evs) > MaxEvBlockCount {
		return ErrEnumeratedValueBlockInvalidCount
	} else if eb.size > MaxEvBlockSize {
		return ErrEnumeratedValueBlockInvalidSize
	}
	return nil
}

// Encode encodes an evblock into a byte buffer
func (eb evblock) Encode() (bts []byte, err error) {
	// check if its valid
	if err = eb.Valid(); err != nil {
		return
	}

	bts = make([]byte, eb.size) //make our buffer

	//encode the header
	binary.LittleEndian.PutUint32(bts, uint32(eb.size))
	binary.LittleEndian.PutUint16(bts[4:], uint16(len(eb.evs)))
	// just make sure its zero, in case something crazy is happening
	bts[6] = 0
	bts[7] = 0

	// now loop on Evs encoding into the buffer
	evb := bts[evblockHeaderLen:] // get a handle on a dedicated buffer we can iterate on
	for _, ev := range eb.evs {
		var n int
		if n, err = ev.encode(evb); err != nil {
			return nil, err
		} else if n > len(evb) {
			return nil, ErrCorruptedEnumeratedValue
		}
		evb = evb[n:]
	}

	return
}

// EncodeWriter encodes an evblock directly into a writer
func (eb evblock) EncodeWriter(w io.Writer) (err error) {
	// check if its valid
	if err = eb.Valid(); err != nil {
		return
	}
	bts := make([]byte, evblockHeaderLen)
	//encode the header
	binary.LittleEndian.PutUint32(bts, uint32(eb.size))
	binary.LittleEndian.PutUint16(bts[4:], uint16(len(eb.evs)))
	// just make sure its zero, in case something crazy is happening
	bts[6] = 0
	bts[7] = 0

	if err = writeAll(w, bts); err != nil {
		return
	}

	//now go write the individual EVs
	for _, ev := range eb.evs {
		if err = ev.EncodeWriter(w); err != nil {
			return
		}
	}

	return
}

// Decode decodes an evblock directly from a buffer
func (eb *evblock) Decode(b []byte) (err error) {
	_, err = eb.decode(b)
	return
}

func (eb *evblock) decode(b []byte) (int, error) {
	eb.size = 0
	if eb.evs != nil {
		eb.evs = eb.evs[0:0]
	}

	//check if the buffer is big enough for the header
	if len(b) < evblockHeaderLen {
		return -1, ErrInvalidBufferSize
	}
	h, err := decodeEvblockHeader(b)
	if err != nil {
		return -1, err
	} else if int(h.size) > len(b) {
		return -1, ErrEnumeratedValueBlockCorrupt
	}

	//advance past the header on the buffer so we can iterate
	total := int(evblockHeaderLen)
	b = b[evblockHeaderLen:]
	for i := uint16(0); i < h.count; i++ {
		var ev EnumeratedValue
		if n, err := ev.decode(b); err != nil {
			return -1, err
		} else if n > len(b) {
			return -1, ErrCorruptedEnumeratedValue
		} else {
			b = b[n:]
			total += n
			eb.evs = append(eb.evs, ev)
		}
	}

	//now check if we actually consumed what the header said we should, this should all match perfectly
	if total != int(h.size) {
		return -1, ErrCorruptedEnumeratedValue
	}
	eb.size = uint64(total)
	return total, nil
}

// DecodeReader decodes an evblock directly from a buffer
func (eb *evblock) DecodeReader(r io.Reader) error {
	sz, err := eb.decodeReader(r)
	if err == nil {
		eb.size = uint64(sz)
		return nil
	}
	return err
}

func (eb *evblock) decodeReader(r io.Reader) (int, error) {
	var h evblockheader
	var err error
	eb.size = 0
	if eb.evs != nil {
		eb.evs = eb.evs[0:0]
	}

	//get the header and check it
	buff := make([]byte, evblockHeaderLen)
	if err = readAll(r, buff); err != nil {
		return -1, nil
	}

	if h, err = decodeEvblockHeader(buff); err != nil {
		return -1, err
	}

	total := int(evblockHeaderLen)
	for i := uint16(0); i < h.count; i++ {
		var ev EnumeratedValue
		var n int
		if n, err = ev.decodeReader(r); err != nil {
			return -1, err
		}
		total += n
		eb.evs = append(eb.evs, ev)
	}

	//now check if we actually consumed what the header said we should, this should all match perfectly
	if total != int(h.size) {
		return -1, ErrCorruptedEnumeratedValue
	}
	return total, nil
}

func decodeEvblockHeader(buff []byte) (h evblockheader, err error) {
	h.size = binary.LittleEndian.Uint32(buff)
	h.count = binary.LittleEndian.Uint16(buff[4:])
	h.delim = binary.LittleEndian.Uint16(buff[6:])
	if h.delim != 0 || h.count > MaxEvBlockCount || h.size > MaxEvBlockSize {
		err = ErrEnumeratedValueBlockCorrupt
		return
	}

	//ok, POTENTIALLY ok
	return
}

func (eb evblock) Compare(eb2 evblock) error {
	if eb.size != eb2.size {
		return fmt.Errorf("mismatch size: %d != %d", eb.size, eb2.size)
	} else if len(eb.evs) != len(eb.evs) {
		return fmt.Errorf("mismatch count: %d != %d", len(eb.evs), len(eb.evs))
	}
	for i := range eb.evs {
		if err := eb.evs[i].Compare(eb2.evs[i]); err != nil {
			return fmt.Errorf("EV compare on %d failed: %v", i, err)
		}
	}
	return nil
}
