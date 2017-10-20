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
	"sync"
)

const (
	EntryBlockHeaderSize = 4 + 4 + 8
	maxEntryBlockSize    = (1024 * 1024 * 1024 * 2) //2GB which is insane
)

var (
	ErrNilEntry          error = errors.New("Cannot add nil entry")
	ErrInvalidKey        error = errors.New("Entry key does not match block")
	ErrBadKey            error = errors.New("EntryBlock key is invalid")
	ErrKeyAlreadySet     error = errors.New("Entry key for block already set")
	ErrInvalidEntryBlock error = errors.New("EntryBlock is invalid")
	ErrBlockTooLarge     error = errors.New("EntryBlock is too large to encode")
	ErrInvalidDestBuff   error = errors.New("EntryBlock buffer is too small")
	ErrInvalidSrcBuff    error = errors.New("Buffer is invalid for an EntryBlock")
	ErrPartialDecode     error = errors.New("Buffer is short/invalid for EntryBlock decode")
)

type EntryBlock struct {
	size    uint64
	key     int64
	entries []*Entry
}

// ActiveEntryBlock represents a back of entries with the same key
type ActiveEntryBlock struct {
	mtx sync.Mutex
	EntryBlock
}

func NewActiveEntryBlock(key int64) *ActiveEntryBlock {
	return &ActiveEntryBlock{
		mtx:        sync.Mutex{},
		EntryBlock: EntryBlock{key: key},
	}
}

func NewEntryBlock(set []*Entry, size uint64) EntryBlock {
	var key int64
	if len(set) != 0 {
		if set[0] != nil {
			key = set[0].TS.Sec
		}
	}
	return EntryBlock{
		size:    size,
		key:     key,
		entries: set,
	}
}

func (aeb *ActiveEntryBlock) Add(e *Entry) error {
	//perform some sanity checks before we lock the block
	if e == nil {
		return ErrNilEntry
	}
	if e.TS.Sec != aeb.key {
		return ErrInvalidKey
	}
	//entry is good, lock and add
	aeb.mtx.Lock()
	aeb.EntryBlock.Add(e)
	aeb.mtx.Unlock()
	return nil
}

func (eb *EntryBlock) Add(e *Entry) {
	eb.size += e.Size()
	eb.entries = append(eb.entries, e)
	if eb.key == 0 {
		eb.key = e.TS.Sec
	}
}

func (eb *EntryBlock) Merge(neb *EntryBlock) error {
	if eb.key != neb.key {
		return ErrBadKey
	}
	eb.size += neb.size
	eb.entries = append(eb.entries, neb.entries...)
	return nil
}

func (eb *EntryBlock) Count() int {
	return len(eb.entries)
}

func (eb *EntryBlock) Entry(i int) *Entry {
	if i >= len(eb.entries) {
		return nil
	}
	return eb.entries[i]
}

func (eb *EntryBlock) Entries() []*Entry {
	return eb.entries
}

type entryBlockHeader struct {
	blockSize  uint32
	entryCount uint32
	key        int64
}

func (ebh entryBlockHeader) encode(b []byte) error {
	if len(b) < int(EntryBlockHeaderSize) {
		return ErrInvalidDestBuff
	}
	binary.LittleEndian.PutUint32(b[0:], ebh.blockSize)
	binary.LittleEndian.PutUint32(b[4:], ebh.entryCount)
	binary.LittleEndian.PutUint64(b[8:], uint64(ebh.key))
	return nil
}

func (ebh *entryBlockHeader) decode(b []byte) error {
	if len(b) < int(EntryBlockHeaderSize) {
		fmt.Println("header too small")
		return ErrInvalidSrcBuff
	}
	ebh.blockSize = binary.LittleEndian.Uint32(b[0:])
	ebh.entryCount = binary.LittleEndian.Uint32(b[4:])
	ebh.key = int64(binary.LittleEndian.Uint64(b[8:]))
	return nil
}

// Encode encodes the EntryBlock to a buffer suitable for transmitting across a network or storing to a file
func (eb *EntryBlock) Encode() ([]byte, error) {
	if eb == nil || len(eb.entries) == 0 || eb.key <= 0 || eb.size <= 0 {
		return nil, ErrInvalidEntryBlock
	}
	if (eb.size + EntryBlockHeaderSize) > maxEntryBlockSize {
		return nil, ErrBlockTooLarge
	}
	//generate a buffer for encoding
	buff := make([]byte, eb.size+EntryBlockHeaderSize)
	if _, err := eb.encodeInto(buff); err != nil {
		return nil, err
	}
	return buff, nil
}

func (eb *EntryBlock) encodeInto(buff []byte) (int, error) {
	hdr := entryBlockHeader{
		blockSize:  uint32(eb.size),
		key:        eb.key,
		entryCount: uint32(len(eb.entries)),
	}
	//encode the header
	if err := hdr.encode(buff[:EntryBlockHeaderSize]); err != nil {
		return 0, err
	}

	return eb.encode(buff[EntryBlockHeaderSize:])
}

func (eb *EntryBlock) EncodeInto(buff []byte) (int, error) {
	if eb == nil || len(eb.entries) == 0 || eb.key <= 0 || eb.size <= 0 {
		return 0, ErrInvalidEntryBlock
	}
	if (eb.size + EntryBlockHeaderSize) > maxEntryBlockSize {
		return 0, ErrBlockTooLarge
	}
	if (eb.size + EntryBlockHeaderSize) > uint64(len(buff)) {
		return 0, ErrInvalidDestBuff
	}
	return eb.encodeInto(buff)
}

// EncodeEntries encodes just the set of entries into the provided buffer
func (eb *EntryBlock) EncodeEntries(buff []byte) (int, error) {
	return eb.encode(buff)
}

func (eb *EntryBlock) encode(buff []byte) (int, error) {
	if uint64(len(buff)) < eb.size {
		return 0, ErrInvalidDestBuff
	}
	//encode each of the entries
	offset := uint64(0)
	for i := range eb.entries {
		sz := eb.entries[i].Size()
		if (offset + sz) > uint64(len(buff)) {
			return 0, ErrInvalidDestBuff
		}
		if err := eb.entries[i].Encode(buff[offset:(offset + sz)]); err != nil {
			return 0, err
		}
		offset += sz
	}
	return int(offset), nil
}

// EncodeAppend takes the current buffer, and appends addional entries to the buffer
// we also update the header
func (eb *EntryBlock) EncodeAppend(buff []byte) ([]byte, error) {
	//decode the original header
	var ebh entryBlockHeader
	if len(buff) > EntryBlockHeaderSize {
		if err := ebh.decode(buff); err != nil {
			return nil, err
		}
	} else {
		//if the input is too small, make a buffer that at least represents a header
		buff = make([]byte, EntryBlockHeaderSize)
	}

	//update the header values
	ebh.blockSize += uint32(eb.size)
	ebh.entryCount += uint32(len(eb.entries))

	//encode the additional items
	b := append(buff, make([]byte, eb.size)...)
	if _, err := eb.encode(b[len(buff):]); err != nil {
		return nil, err
	}

	//update the header
	if err := ebh.encode(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Decode will decode an EntryBlock from a buffer, with error checking
func (eb *EntryBlock) Decode(b []byte) error {
	if len(b) < EntryBlockHeaderSize {
		fmt.Println("header too small 2", len(b))
		return ErrInvalidSrcBuff
	}
	var ebh entryBlockHeader
	if err := ebh.decode(b); err != nil {
		return err
	}
	if ebh.blockSize > maxEntryBlockSize {
		return ErrBlockTooLarge
	}
	if ebh.blockSize+EntryBlockHeaderSize != uint32(len(b)) {
		fmt.Println(ebh.blockSize, EntryBlockHeaderSize, len(b))
		return ErrInvalidSrcBuff
	}

	offset := uint64(EntryBlockHeaderSize)
	blen := uint64(len(b))
	var sz uint32

	for i := uint32(0); i < ebh.entryCount; i++ {
		var ent Entry
		n, err := ent.DecodeHeader(b[offset:])
		if err != nil {
			return err
		}
		dlen := uint64(n)
		if (dlen + uint64(ENTRY_HEADER_SIZE) + offset) > blen {
			fmt.Printf("%d/%d %d > %d\n", i, ebh.entryCount, (dlen + uint64(ENTRY_HEADER_SIZE) + offset), blen)
			return ErrInvalidSrcBuff
		}
		offset += uint64(ENTRY_HEADER_SIZE)
		ent.Data = b[offset : offset+dlen]
		offset += dlen
		eb.entries = append(eb.entries, &ent)
		sz += uint32(ent.Size())
	}
	if offset != uint64(len(b)) {
		return ErrPartialDecode
	}
	if sz != ebh.blockSize {
		return ErrPartialDecode
	}
	eb.size = uint64(sz)
	eb.key = ebh.key

	return nil
}

func (aeb *ActiveEntryBlock) Decode(b []byte) error {
	aeb.size = 0
	if err := aeb.EntryBlock.Decode(b); err != nil {
		return err
	}
	return nil
}

func (eb *EntryBlock) SetKey(k EntryKey) error {
	if eb.key > 0 {
		return ErrKeyAlreadySet
	}
	if k <= 0 {
		return ErrBadKey
	}
	eb.key = int64(k)
	return nil
}

func (eb EntryBlock) Size() uint64 {
	return eb.size
}

func (eb EntryBlock) EncodedSize() uint64 {
	return eb.size + EntryBlockHeaderSize
}

func (eb EntryBlock) Len() int {
	return len(eb.entries)
}

func (eb EntryBlock) Key() int64 {
	return eb.key
}

func (eb EntryBlock) EntryKey(i int) (int64, error) {
	if i >= len(eb.entries) {
		return -1, errors.New("invalid index")
	}
	return eb.entries[i].TS.Sec, nil
}
