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
	EVBlockHeaderLen = 8
	MaxEvBlockCount  = 0xffff           //bonkers, but we need to be safe
	MaxEvBlockSize   = 1024 * 1024 * 32 //not too lean, but let's remove opportunity for crazy memory pressure

)

var (
	ErrEnumeratedValueBlockInvalidCount = errors.New("enumerated value block has too many evs")
	ErrEnumeratedValueBlockInvalidSize  = errors.New("enumerated value block is too large")
	ErrEnumeratedValueBlockCorrupt      = errors.New("enumerated value block buffer is corrupted")
)

// EVBlockHeader type expressed for documentation, defines transport header for evlocks.
type EVBlockHeader struct {
	Size  uint32 //this is the complete size, including the header
	Count uint16
	pad   uint16 //used as a bit of an encoding sanity check and to get word alignment
}

// evblock is a block of enumerated values, this is used to help with transporting enumerated values
// over the wire and to guard against users of the API doing wonky things that become expensive
// to encode/decode.
type evblock struct {
	// keep the size running as we go so encoding is less expensive to ask for it
	// size is the encoded size, it includes the header, we keep this so its easy
	// to pre-allocate buffers when needed
	size uint64
	evs  []EnumeratedValue
}

// Add adds an enumerated value to an evbloc, this function keeps a running tally of size for fast query.
func (eb *evblock) Add(ev EnumeratedValue) {
	if eb.size == 0 {
		eb.fastAdd(ev)
	} else {
		eb.updateEv(ev)
	}
}

// fastAdd is a fast path adder where there are no evs attached and we can just add this quickly
func (eb *evblock) fastAdd(ev EnumeratedValue) {
	eb.size = EVBlockHeaderLen + uint64(ev.Size())
	eb.evs = []EnumeratedValue{ev}
}

func (eb *evblock) updateEv(ev EnumeratedValue) {
	for i, x := range eb.evs {
		if x.Name == ev.Name {
			//update existing
			eb.size -= uint64(x.Size())
			eb.evs[i] = ev
			eb.size += uint64(ev.Size())
			return
		}
	}
	//if we hit here it wasn't found
	eb.size += uint64(ev.Size())
	eb.evs = append(eb.evs, ev)
}

// AddSet adds a slice of enumerated value to an evbloc, this function keeps a running tally of size for fast query.
func (eb *evblock) AddSet(evs []EnumeratedValue) {
	if eb.size == 0 {
		eb.fastAddSet(evs)
	} else {
		//do the slow crappy way where we are updating evs
		for _, ev := range evs {
			eb.updateEv(ev)
		}
	}
}

// fastAddSet is a fast path operation when there are no EVs and we basically just get to set them
func (eb *evblock) fastAddSet(evs []EnumeratedValue) {
	eb.size = EVBlockHeaderLen
	eb.evs = make([]EnumeratedValue, 0, len(evs))
	for _, ev := range evs {
		eb.size += uint64(ev.Size())
		eb.evs = append(eb.evs, ev)
	}
}

// Size is just a helper accessor to help with encoding efficiency.
func (eb evblock) Size() uint64 {
	return eb.size
}

// Count is just a helper accessor to spit out the number of EVs in the block.
func (eb evblock) Count() int {
	return len(eb.evs)
}

// Populated is a helper to check if there are any EVs.
func (eb evblock) Populated() bool {
	return eb.size > 0
}

// Reset resets the entry block, the underlying slice is not freed.
func (eb *evblock) Reset() {
	eb.size = 0
	eb.evs = eb.evs[0:0]
}

// Values is an accessor to actualy get the set of enumerated values out of the evblock
// this returns the slice directly, so callers COULD mess with the slice and break the size
// tracker.  Basically don't re-use or assign to this slice, if you do the evblock you pulled it from
// is no longer valid.
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

// Get retrieves an enumerated value from the set using a name
// if the name does not exist an empty  EnumeratedValue and ok = false will be returned
func (eb evblock) Get(name string) (ev EnumeratedValue, ok bool) {
	for i := range eb.evs {
		if eb.evs[i].Name == name {
			ev = eb.evs[i]
			ok = true
			break
		}
	}
	return
}

// Append appends one evblock to another.  This function DOES NOT de-duplicate enumerated values
// if the src block already has foobar and so does the destination it will be duplicated
func (eb *evblock) Append(seb evblock) {
	for _, v := range seb.evs {
		if v.Valid() {
			eb.Add(v)
		}
	}
	return
}

// Encode encodes an evblock into a byte buffer.
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
	evb := bts[EVBlockHeaderLen:] // get a handle on a dedicated buffer we can iterate on
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

// EncodeBuffer encodes an evblock into a caller provided byte buffer
// and returns the number of bytes consumed and a potential error.
func (eb evblock) EncodeBuffer(bts []byte) (r int, err error) {
	// check if its valid
	if err = eb.Valid(); err != nil {
		return
	}
	if len(bts) < int(eb.size) {
		err = ErrInvalidBufferSize
		return
	}

	//encode the header
	binary.LittleEndian.PutUint32(bts, uint32(eb.size))
	binary.LittleEndian.PutUint16(bts[4:], uint16(len(eb.evs)))
	// just make sure its zero, in case something crazy is happening
	bts[6] = 0
	bts[7] = 0

	r = EVBlockHeaderLen

	// now loop on Evs encoding into the buffer
	evb := bts[EVBlockHeaderLen:] // get a handle on a dedicated buffer we can iterate on
	for _, ev := range eb.evs {
		var n int
		if n, err = ev.encode(evb); err != nil {
			return -1, err
		} else if n > len(evb) {
			return -1, ErrCorruptedEnumeratedValue
		}
		r += n
		evb = evb[n:]
	}

	return
}

// EncodeWriter encodes an evblock directly into a writer
// and returns the number of bytes consumed and a potential error.
func (eb evblock) EncodeWriter(w io.Writer) (r int, err error) {
	// check if its valid
	if err = eb.Valid(); err != nil {
		return
	}
	bts := make([]byte, EVBlockHeaderLen)
	//encode the header
	binary.LittleEndian.PutUint32(bts, uint32(eb.size))
	binary.LittleEndian.PutUint16(bts[4:], uint16(len(eb.evs)))
	// just make sure its zero, in case something crazy is happening
	bts[6] = 0
	bts[7] = 0

	if err = writeAll(w, bts); err != nil {
		return
	}
	r = EVBlockHeaderLen

	//now go write the individual EVs
	for _, ev := range eb.evs {
		if n, err := ev.EncodeWriter(w); err != nil {
			return -1, err
		} else {
			r += n
		}
	}

	return
}

// Decode decodes an evblock directly from a buffer and returns the number of bytes consumed.
// This function will copy all referenced memory so the underlying buffer can be re-used.
func (eb *evblock) Decode(b []byte) (int, error) {
	eb.size = 0
	if eb.evs != nil {
		eb.evs = eb.evs[0:0]
	}

	//check if the buffer is big enough for the header
	if len(b) < EVBlockHeaderLen {
		return -1, ErrInvalidBufferSize
	}
	h, err := DecodeEVBlockHeader(b)
	if err != nil {
		return -1, err
	} else if int(h.Size) > len(b) {
		return -1, ErrEnumeratedValueBlockCorrupt
	}

	//advance past the header on the buffer so we can iterate
	total := int(EVBlockHeaderLen)
	b = b[EVBlockHeaderLen:]
	for i := uint16(0); i < h.Count; i++ {
		var ev EnumeratedValue
		if n, err := ev.Decode(b); err != nil {
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
	if total != int(h.Size) {
		return -1, ErrCorruptedEnumeratedValue
	}
	eb.size = uint64(total)
	return total, nil
}

// DecodeAlt decodes an evblock directly from a buffer and returns the number of bytes consumed.
// All data is directly referenced to the provided buffer, the buffer cannot be re-used while any numerated
// value is still in use.
func (eb *evblock) DecodeAlt(b []byte) (int, error) {
	eb.size = 0
	if eb.evs != nil {
		eb.evs = eb.evs[0:0]
	}

	//check if the buffer is big enough for the header
	if len(b) < EVBlockHeaderLen {
		return -1, ErrInvalidBufferSize
	}
	h, err := DecodeEVBlockHeader(b)
	if err != nil {
		return -1, err
	} else if int(h.Size) > len(b) {
		return -1, ErrEnumeratedValueBlockCorrupt
	}

	//advance past the header on the buffer so we can iterate
	total := int(EVBlockHeaderLen)
	b = b[EVBlockHeaderLen:]
	for i := uint16(0); i < h.Count; i++ {
		var ev EnumeratedValue
		if n, err := ev.DecodeAlt(b); err != nil {
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
	if total != int(h.Size) {
		return -1, ErrCorruptedEnumeratedValue
	}
	eb.size = uint64(total)
	return total, nil
}

// DecodeReader decodes an evblock directly from a buffer and returns the number of bytes read and a potential error.
func (eb *evblock) DecodeReader(r io.Reader) (int, error) {
	var h EVBlockHeader
	var err error
	eb.size = 0
	if eb.evs != nil {
		eb.evs = eb.evs[0:0]
	}

	//get the header and check it
	buff := make([]byte, EVBlockHeaderLen)
	if err = readAll(r, buff); err != nil {
		return -1, nil
	}

	if h, err = DecodeEVBlockHeader(buff); err != nil {
		return -1, err
	}

	total := int(EVBlockHeaderLen)
	for i := uint16(0); i < h.Count; i++ {
		var ev EnumeratedValue
		var n int
		if n, err = ev.DecodeReader(r); err != nil {
			return -1, err
		}
		total += n
		eb.evs = append(eb.evs, ev)
	}

	//now check if we actually consumed what the header said we should, this should all match perfectly
	if total != int(h.Size) {
		return -1, ErrCorruptedEnumeratedValue
	}
	eb.size = uint64(total)
	return total, nil
}

// DecodeEVBlockHeader decodes an EVBlockHeader from a buffer and validates it.
// An empty EVBlockHeader and error is returned if the buffer or resulting EVBlockHeader is invalid.
func DecodeEVBlockHeader(buff []byte) (h EVBlockHeader, err error) {
	h.Size = binary.LittleEndian.Uint32(buff)
	h.Count = binary.LittleEndian.Uint16(buff[4:])
	h.pad = binary.LittleEndian.Uint16(buff[6:])
	if h.pad != 0 || h.Count > MaxEvBlockCount || h.Size > MaxEvBlockSize {
		err = ErrEnumeratedValueBlockCorrupt
		return
	}

	//ok, POTENTIALLY ok
	return
}

// Compare compares two evblocks and returns an error describing the differences if there are any.
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

// DeepCopy performs a deep copy of an evblock so that any handles on underlying bytes are discarded.
// This function is expensive, use sparingly.
func (eb evblock) DeepCopy() (r evblock) {
	if eb.size == 0 || len(eb.evs) == 0 {
		return
	}
	r = evblock{
		size: eb.size,
		evs:  make([]EnumeratedValue, 0, len(eb.evs)),
	}
	for _, ev := range eb.evs {
		r.evs = append(r.evs, EnumeratedValue{
			Name: ev.Name, //strings are immuntable, no need to copy
			Value: EnumeratedData{
				evtype: ev.Value.evtype,
				data:   append([]byte(nil), ev.Value.data...),
			},
		})
	}
	return
}
