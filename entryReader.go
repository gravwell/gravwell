/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bufio"
	"errors"
	"github.com/gravwell/ingest/entry"
	"io"
	"net"
	"sync"
	"unsafe"
)

const (
	READ_BUFFER_SIZE int = 4 * 1024 * 1024

	entCacheRechargeSize int = 2048
)

var (
	errFailedFullRead = errors.New("Failed to read full buffer")
)

type EntryReader struct {
	conn       net.Conn
	bIO        *bufio.Reader
	bAckWriter *bufio.Writer
	errCount   uint32
	mtx        *sync.Mutex
	hot        bool
	buff       []byte
	//entCache is used to allocate entries in blocks to relieve some pressure on the allocator and GC
	entCache    []entry.Entry
	entCacheIdx int
}

func NewEntryReader(conn net.Conn) (*EntryReader, error) {

	//buffer big enough store entire entry header + EntryID
	return &EntryReader{
		conn:       conn,
		bIO:        bufio.NewReaderSize(conn, READ_BUFFER_SIZE),
		bAckWriter: bufio.NewWriterSize(conn, (MAX_UNCONFIRMED_COUNT/2)*ACK_SIZE),
		mtx:        &sync.Mutex{},
		hot:        true,
		buff:       make([]byte, READ_ENTRY_HEADER_SIZE),
	}, nil
}

func (er *EntryReader) Close() error {
	// lock and unlock
	er.mtx.Lock()
	defer er.mtx.Unlock()

	if !er.hot {
		return errors.New("Close on closed EntryTransport")
	}
	if err := er.bAckWriter.Flush(); err != nil {
		return err
	}

	er.hot = false
	return nil
}

func (er *EntryReader) Read() (*entry.Entry, error) {
	var (
		err error
		sz  uint32
		id  EntrySendID
	)
	er.mtx.Lock()
	defer er.mtx.Unlock()
	if er.entCacheIdx >= len(er.entCache) {
		er.entCache = make([]entry.Entry, entCacheRechargeSize)
		er.entCacheIdx = 0
	}
	ent := &er.entCache[er.entCacheIdx]

	if err = er.fillHeader(ent, &id, &sz); err != nil {
		return nil, err
	}
	ent.Data = make([]byte, sz)
	if _, err = io.ReadFull(er.bIO, ent.Data); err != nil {
		return nil, err
	}
	if err = er.throwAck(id); err != nil {
		return nil, err
	}
	er.entCacheIdx++
	return ent, nil
}

// we just eat bytes until we hit the magic number,  this is a rudimentary
// error recovery where a bad read can skip the entry
func (er *EntryReader) fillHeader(ent *entry.Entry, id *EntrySendID, sz *uint32) error {
	var err error
	var n int
	//read the "new entry" magic number
headerLoop:
	for {
		n, err = io.ReadFull(er.bIO, er.buff[0:4])
		if err != nil {
			return err
		}
		if n < 4 {
			return errFailedFullRead
		}
		switch *(*uint32)(unsafe.Pointer(&er.buff[0])) {
		case FORCE_ACK_MAGIC:
			if err := er.bAckWriter.Flush(); err != nil {
				return err
			}
		case NEW_ENTRY_MAGIC:
			break headerLoop
		default:
			continue
		}
	}
	//read entry header worth as well as id (64bit)
	n, err = io.ReadFull(er.bIO, er.buff[:entry.ENTRY_HEADER_SIZE+8])
	if err != nil {
		return err
	}
	dataSize, err := ent.DecodeHeader(er.buff)
	if err != nil {
		return err
	}
	if dataSize > int(MAX_ENTRY_SIZE) {
		return errors.New("Entry size too large")
	}
	*sz = uint32(dataSize) //dataSize is a uint32 internally, so these casts are OK
	*id = *(*EntrySendID)(unsafe.Pointer(&er.buff[entry.ENTRY_HEADER_SIZE]))
	return nil
}

// throwAck must be called with the mutex already locked by parent
func (er *EntryReader) throwAck(id EntrySendID) error {
	//check if we should flush our ack buffer
	if er.bAckWriter.Available() < ACK_SIZE {
		if err := er.bAckWriter.Flush(); err != nil {
			return err
		}
	}

	//fill out the buffer
	*(*uint32)(unsafe.Pointer(&er.buff[0])) = CONFIRM_ENTRY_MAGIC
	*(*EntrySendID)(unsafe.Pointer(&er.buff[4])) = id
	n, err := er.bAckWriter.Write(er.buff[0:ACK_SIZE])
	if err != nil {
		return err
	} else if n != ACK_SIZE {
		return errors.New("Failed to send ACK")
	}
	return nil
}
