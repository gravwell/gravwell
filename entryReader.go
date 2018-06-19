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
	"encoding/binary"
	"errors"
	"github.com/gravwell/ingest/entry"
	"io"
	"net"
	"sync"
	"time"
)

const (
	READ_BUFFER_SIZE int = 4 * 1024 * 1024
	//TODO - we should really discover the MTU of the link and use that
	ACK_WRITER_BUFFER_SIZE int = (ackEncodeSize * MAX_UNCONFIRMED_COUNT)

	entCacheRechargeSize int = 2048
	ackChanSize          int = MAX_UNCONFIRMED_COUNT
	maxAckCommandSize    int = 4 + 8
	ackEncodeSize        int = 4 + 8 //cmd plus uint64 ID value
	throttleEncodeSize   int = 4 + 8 //cmd plus uint64 duration value
	pongEncodeSize       int = 4     //cmd
)

var (
	errFailedFullRead      = errors.New("Failed to read full buffer")
	errAckRoutineClosed    = errors.New("Ack writer is closed")
	errUnknownCommand      = errors.New("Unknown command id")
	errBufferTooSmall      = errors.New("Buffer too small for encoded command")
	errFailBufferTooSmall  = errors.New("Buffer too small for encoded command")
	errFailedToReadCommand = errors.New("Failed to read command")

	ackBatchReadTimerDuration = 10 * time.Millisecond
	defaultReaderTimeout      = 10 * time.Minute

	nilTime time.Time
)

type ackCommand struct {
	cmd IngestCommand
	val uint64 //this can be converted to any number of things, id, time.Duration, etc...
}

type EntryReader struct {
	conn       net.Conn
	bIO        *bufio.Reader
	bAckWriter *bufio.Writer
	errCount   uint32
	mtx        *sync.Mutex
	wg         *sync.WaitGroup
	ackChan    chan ackCommand
	errState   error
	hot        bool
	started    bool
	buff       []byte
	//entCache is used to allocate entries in blocks to relieve some pressure on the allocator and GC
	entCache    []entry.Entry
	entCacheIdx int
	opCount     uint64
	lastCount   uint64
	timeout     time.Duration
}

func NewEntryReader(conn net.Conn) (*EntryReader, error) {

	//buffer big enough store entire entry header + EntryID
	return &EntryReader{
		conn:       conn,
		bIO:        bufio.NewReaderSize(conn, READ_BUFFER_SIZE),
		bAckWriter: bufio.NewWriterSize(conn, ACK_WRITER_BUFFER_SIZE),
		mtx:        &sync.Mutex{},
		wg:         &sync.WaitGroup{},
		ackChan:    make(chan ackCommand, ackChanSize),
		hot:        true,
		buff:       make([]byte, READ_ENTRY_HEADER_SIZE),
		timeout:    defaultReaderTimeout,
	}, nil
}

func (er *EntryReader) Start() error {
	er.mtx.Lock()
	defer er.mtx.Unlock()
	//if the entry reader has been closed, we can't Start it
	if !er.hot {
		return errors.New("EntryReader closed")
	}
	//we don't support stopping and restarting entry readers
	if er.started {
		return errors.New("Already started")
	}
	//resetting the timeout will update/set it if it isn't already
	if err := er.resetTimeout(); err != nil {
		return err
	}
	er.started = true
	er.wg.Add(1)
	go er.ackRoutine()
	return nil
}

func (er *EntryReader) Close() error {
	// lock and unlock
	er.mtx.Lock()
	defer er.mtx.Unlock()

	if !er.hot {
		return errors.New("Close on closed EntryTransport")
	}
	if er.started {
		//close the ack channel and wait for the routine to return
		close(er.ackChan)
		//wait for the ack writer routine to close
		//the ack writer will flush on its way out
		er.wg.Wait()
	}
	if err := er.bAckWriter.Flush(); err != nil {
		return err
	}

	er.hot = false

	return nil
}

func (er *EntryReader) Read() (e *entry.Entry, err error) {
	er.mtx.Lock()
	if e, err = er.read(); err == nil {
		er.opCount++
	} else if isTimeout(err) {
		//if its a timeout and nothing new came in, bail
		if er.opCount == er.lastCount {
			err = io.EOF
		} else {
			//we have had new data/requests, reset the deadline
			if err = er.resetTimeout(); err != nil {
				er.mtx.Unlock()
				return
			}
			//re-attempt the read
			if e, err = er.read(); err != nil {
				if isTimeout(err) {
					err = io.EOF
				}
			} else {
				//good read, up the op count and reset the lastCount
				er.opCount++
				er.lastCount = er.opCount
			}
		}
	}
	er.mtx.Unlock()
	return e, err
}

//reset the read deadline on the underlying connection, caller must hold the lock
func (er *EntryReader) resetTimeout() error {
	if er.timeout <= 0 {
		return er.conn.SetReadDeadline(nilTime)
	}
	return er.conn.SetReadDeadline(time.Now().Add(er.timeout))
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if nerr, ok := err.(net.Error); ok {
		return nerr.Timeout()
	}
	return false
}

func (er *EntryReader) read() (*entry.Entry, error) {
	var (
		err error
		sz  uint32
		id  entrySendID
	)
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
func (er *EntryReader) fillHeader(ent *entry.Entry, id *entrySendID, sz *uint32) error {
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
		switch IngestCommand(binary.LittleEndian.Uint32(er.buff[0:])) {
		case FORCE_ACK_MAGIC:
			if err := er.forceAck(); err != nil {
				return err
			}
		case NEW_ENTRY_MAGIC:
			break headerLoop
		default: //we should probably bail out if we get desyned
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
	*id = entrySendID(binary.LittleEndian.Uint64(er.buff[entry.ENTRY_HEADER_SIZE:]))
	return nil
}

// forceAck just sends a 0 down the ID channel, the ack routine should see it an force everything out
func (er *EntryReader) forceAck() error {
	return er.throwAck(0)
}

func discard(c chan ackCommand) {
	for _ = range c {
		//do nothing
	}
}

func (er *EntryReader) routineCleanFail(err error) {
	//set the error state
	er.errState = err
	//close the connection
	er.conn.Close()
	//feed until the channel closes
	//to prevent deadlock
	discard(er.ackChan)
}

func (er *EntryReader) ackRoutine() {
	defer er.wg.Done()
	//escape analysis should ensure this is on the stack
	buff := make([]byte, ACK_WRITER_BUFFER_SIZE)

	for v := range er.ackChan {
		if err := er.fillAndSendAckBuffer(buff, v); err != nil {
			er.routineCleanFail(err)
			return
		}
	}
}

func (er *EntryReader) fillAndSendAckBuffer(b []byte, v ackCommand) (err error) {
	var off int
	var n int
	var flush bool
	var ok bool
	//encode value into the buffer
	if off, flush, err = v.encode(b); err != nil {
		return
	} else if flush {
		err = er.bAckWriter.Flush()
		return
	}

	//fire up a timer and start feeding
	tmr := time.NewTimer(ackBatchReadTimerDuration)
	defer tmr.Stop()
feedLoop:
	for {
		select {
		case v, ok = <-er.ackChan:
			if !ok {
				break
			}
			//check that we have room
			if (v.size() + off) >= len(b) {
				//ok, flush and keep rolling
				if err = er.writeAll(b[:off]); err != nil {
					return
				}
				if err = er.bAckWriter.Flush(); err != nil {
					return
				}
				off = 0
			}
			//encode
			if n, flush, err = v.encode(b[off:]); err != nil {
				return
			}
			off += n
			//if we hit the size of our buffer, break
			if flush || off == len(b) {
				break
			}
		case _ = <-tmr.C:
			break feedLoop
		}
	}
	if off > 0 {
		if err = er.writeAll(b[:off]); err != nil {
			return
		}
		err = er.bAckWriter.Flush()
	}
	return
}

func (er *EntryReader) SendThrottle(d time.Duration) error {
	if !er.started {
		return errAckRoutineClosed
	}
	er.ackChan <- ackCommand{cmd: THROTTLE_MAGIC, val: uint64(d)}
	return nil
}

// throwAck throws an ack down the ackChan for the ack writer to encode and write
// throwAck must be called with the mutex already locked by parent
func (er *EntryReader) throwAck(id entrySendID) error {
	if !er.started {
		return errAckRoutineClosed
	}
	er.ackChan <- ackCommand{cmd: CONFIRM_ENTRY_MAGIC, val: uint64(id)}
	return nil
}

func (er *EntryReader) writeAll(b []byte) error {
	var written int
	for written < len(b) {
		n, err := er.bAckWriter.Write(b[written:])
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		written += n
	}
	if written != len(b) {
		return errors.New("Failed to write entire buffer")
	}
	return nil
}

func (ac ackCommand) isFlush() bool {
	return ac.cmd == FORCE_ACK_MAGIC || (ac.cmd == CONFIRM_ENTRY_MAGIC && ac.val == 0)
}

func (ac ackCommand) size() int {
	switch ac.cmd {
	case FORCE_ACK_MAGIC:
		return 0 //we shouldn't ever really do anything with this
	case CONFIRM_ENTRY_MAGIC:
		if ac.val == 0 {
			return 0
		}
		return ackEncodeSize
	case PONG_MAGIC:
		return 4
	case THROTTLE_MAGIC:
		return throttleEncodeSize
	}
	return 0
}

func (ac ackCommand) encode(b []byte) (n int, flush bool, err error) {
	switch ac.cmd {
	case FORCE_ACK_MAGIC:
		flush = true
	case CONFIRM_ENTRY_MAGIC:
		if ac.val == 0 {
			flush = true
			return
		}
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		binary.LittleEndian.PutUint64(b[4:], ac.val)
		n += ackEncodeSize
	case PONG_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		n += pongEncodeSize
		flush = true
	case THROTTLE_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		binary.LittleEndian.PutUint64(b[4:], ac.val)
		n += throttleEncodeSize
		flush = true
	default:
		err = errUnknownCommand
	}
	return
}

func (ac *ackCommand) decode(rdr *bufio.Reader, blocking bool) (ok bool, err error) {
	cmd := make([]byte, 4)
	val := make([]byte, 8)
	n := rdr.Buffered()
	//if we are not blocking and there is not enough data to get a full command, just bail
	if n < 4 && !blocking {
		return
	}
	if _, err = io.ReadFull(rdr, cmd); err != nil {
		return
	}
	ac.cmd = IngestCommand(binary.LittleEndian.Uint32(cmd))
	switch ac.cmd {
	case THROTTLE_MAGIC:
		fallthrough
	case CONFIRM_ENTRY_MAGIC:
		if _, err = io.ReadFull(rdr, val); err != nil {
			return
		}
		ac.val = binary.LittleEndian.Uint64(val)
		ok = true
	case PONG_MAGIC:
		ok = true
	default:
		err = errUnknownCommand
	}
	return
}
