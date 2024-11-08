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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/klauspost/compress/snappy"
)

const (
	READ_BUFFER_SIZE int = 4 * 1024 * 1024
	//TODO - we should really discover the MTU of the link and use that
	ACK_WRITER_BUFFER_SIZE int = (ackEncodeSize * MAX_UNCONFIRMED_COUNT)
	ACK_WRITER_CAN_WRITE   int = (ackEncodeSize * (MAX_UNCONFIRMED_COUNT / 2))

	entCacheRechargeSize int = 2048
	ackChanSize          int = MAX_UNCONFIRMED_COUNT
	maxAckCommandSize    int = 4 + 8
	ackEncodeSize        int = 4 + 8 //cmd plus uint64 ID value
	throttleEncodeSize   int = 4 + 8 //cmd plus uint64 duration value
	confirmTagSize       int = 4 + 8
	pongEncodeSize       int = 4 //cmd
	minBufferSize        int = 1024
)

var (
	errFailedFullRead      = errors.New("Failed to read full buffer")
	errAckRoutineClosed    = errors.New("Ack writer is closed")
	errUnknownCommand      = errors.New("Unknown command id")
	errBufferTooSmall      = errors.New("Buffer too small for encoded command")
	errFailBufferTooSmall  = errors.New("Buffer too small for encoded command")
	errFailedToReadCommand = errors.New("Failed to read command")
	ErrOversizedEntry      = errors.New("Entry data exceeds maximum size")

	ackBatchReadTimerDuration = 10 * time.Millisecond
	defaultReaderTimeout      = 10 * time.Minute
	keepAliveInterval         = 1000 * time.Millisecond
	closeTimeout              = 10 * time.Second

	nilTime time.Time
)

type TagManager interface {
	GetAndPopulate(string) (entry.EntryTag, error)
}

type ackCommand struct {
	cmd IngestCommand
	val uint64 //this can be converted to any number of things, id, time.Duration, etc...
}

type EntryReader struct {
	conn       net.Conn
	flshr      flusher
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
	opCount    uint64
	lastCount  uint64
	timeout    time.Duration
	tagMan     TagManager
	// the reader stores some info about the other side
	igName         string
	igVersion      string
	igUUID         string
	igAPIVersion   uint16
	igStateMtx     *sync.Mutex
	igState        IngesterState           // the most recent state message received
	stateCallbacks []IngesterStateCallback // functions to be called when an IngesterState message is received
}

func NewEntryReader(conn net.Conn) (*EntryReader, error) {
	cfg := EntryReaderWriterConfig{
		Conn:                  conn,
		OutstandingEntryCount: MAX_UNCONFIRMED_COUNT,
		BufferSize:            READ_BUFFER_SIZE,
		Timeout:               defaultReaderTimeout,
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return NewEntryReaderEx(cfg)
}

func NewEntryReaderEx(cfg EntryReaderWriterConfig) (*EntryReader, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	//buffer big enough store entire entry header + EntryID
	return &EntryReader{
		conn:       cfg.Conn,
		bIO:        bufio.NewReaderSize(cfg.Conn, cfg.BufferSize),
		bAckWriter: bufio.NewWriterSize(cfg.Conn, ackEncodeSize*cfg.OutstandingEntryCount),
		mtx:        &sync.Mutex{},
		wg:         &sync.WaitGroup{},
		ackChan:    make(chan ackCommand, cfg.OutstandingEntryCount),
		hot:        true,
		buff:       make([]byte, READ_ENTRY_HEADER_SIZE),
		timeout:    cfg.Timeout,
		tagMan:     cfg.TagMan,
		igStateMtx: &sync.Mutex{},
	}, nil
}

// SetTagManager gives a handle on the instantiator's tag management system.
// If this is not set, tags cannot be negotiated on the fly
func (er *EntryReader) SetTagManager(tm TagManager) {
	er.tagMan = tm
}

func (er *EntryReader) GetIngesterInfo() (string, string, string) {
	return er.igName, er.igVersion, er.igUUID
}

func (er *EntryReader) GetIngesterAPIVersion() uint16 {
	return er.igAPIVersion
}

// GetIngesterState returns the most recent state object received from the ingester.
func (er *EntryReader) GetIngesterState() (is IngesterState) {
	if er != nil {
		er.igStateMtx.Lock()
		is = er.igState.Copy() // we must copy out the ingest state because it holds a map
		er.igStateMtx.Unlock()
	}
	return
}

type IngesterStateCallback func(IngesterState)

// AddIngesterStateCallback registers a callback function which will be called every
// time the EntryReader reads an IngesterState message from the client.
// Calling AddIngesterStateCallback multiple times will add additional callbacks to the
// list.
// Warning: If a callback hangs, the entire entry reader will hang.
func (er *EntryReader) AddIngesterStateCallback(f IngesterStateCallback) {
	er.stateCallbacks = append(er.stateCallbacks, f)
}

// configureStream will
func (er *EntryReader) ConfigureStream() (err error) {
	var req StreamConfiguration
	er.mtx.Lock()
	defer er.mtx.Unlock()

	if er.igAPIVersion < MINIMUM_DYN_CONFIG_VERSION {
		//just return quietly, its ok
		return
	}
	//set our timeouts and perform the exchange
	if err = req.Read(er.bIO); err != nil {
		return
	} else if err = req.validate(); err != nil {
		return
	} else if err = req.Write(er.bAckWriter); err != nil {
		return
	} else if err = er.bAckWriter.Flush(); err != nil {
		return
	} else if err = er.resetTimeout(); err != nil {
		return
	}

	//we are in good shape, configure the stream
	if req.Compression != CompressNone {
		err = er.startCompression(req.Compression)
	}
	return
}

// startCompression gets the entryReader/Writer ready to work with a compressed connection
// caller MUST HOLD THE LOCK
func (ew *EntryReader) startCompression(ct CompressionType) (err error) {
	switch ct {
	case CompressNone: //do nothing
	case CompressSnappy:
		//get a writer rolling
		wtr := snappy.NewWriter(ew.conn)
		ew.flshr = wtr
		ew.bAckWriter.Reset(wtr)
		//get a reader rolling
		ew.bIO.Reset(snappy.NewReader(ew.conn))
	default:
		err = fmt.Errorf("Unknown compression id %x", ct)
	}
	return
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
	//try to set a deadline on the connection, we are exiting so everything BETTER wrap up within our closeTimeout
	er.conn.SetDeadline(time.Now().Add(closeTimeout))
	defer er.conn.SetDeadline(nilTime)

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
	} else if isTimeout(err) || err == syscall.EPIPE {
		err = io.EOF
	}
	er.mtx.Unlock()
	return e, err
}

// reset the read deadline on the underlying connection, caller must hold the lock
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
		err    error
		sz     uint32
		id     entrySendID
		hasEvs bool
	)
	ent := &entry.Entry{}

	if err = er.fillHeader(ent, &id, &sz, &hasEvs); err != nil {
		return nil, err
	}
	ent.Data = make([]byte, sz)
	if _, err = io.ReadFull(er.bIO, ent.Data); err != nil {
		return nil, err
	} else if hasEvs {
		if err = ent.ReadEVs(er.bIO); err != nil {
			return nil, err
		}
	}
	if err = er.throwAck(id); err != nil {
		return nil, err
	}
	return ent, nil
}

// we just eat bytes until we hit the magic number,  this is a rudimentary
// error recovery where a bad read can skip the entry
func (er *EntryReader) fillHeader(ent *entry.Entry, id *entrySendID, sz *uint32, evs *bool) error {
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

		// Every time we read something, reset the timeout
		if err := er.resetTimeout(); err != nil {
			return err
		}

		switch IngestCommand(binary.LittleEndian.Uint32(er.buff[0:])) {
		case FORCE_ACK_MAGIC:
			if err := er.forceAck(); err != nil {
				return err
			}
		case NEW_ENTRY_MAGIC:
			break headerLoop
		case TAG_MAGIC:
			// read length of string
			n, err = io.ReadFull(er.bIO, er.buff[0:4])
			if err != nil {
				return err
			}
			if n < 4 {
				return errFailedFullRead
			}
			length := binary.LittleEndian.Uint32(er.buff[0:4])
			//ensure we aren't getting something crazy
			if int(length) > MAX_TAG_LENGTH {
				return ErrOversizedTag
			}
			name := make([]byte, length)
			n, err = io.ReadFull(er.bIO, name)
			if err != nil {
				return err
			}
			if n < int(length) {
				return errFailedFullRead
			} else if err := CheckTag(string(name)); err != nil {
				er.ackChan <- ackCommand{cmd: ERROR_TAG_MAGIC, val: uint64(0)}
				continue
			}

			// Now that we've read, we can either send back a CONFIRM
			// or an ERROR
			if er.tagMan == nil {
				er.ackChan <- ackCommand{cmd: ERROR_TAG_MAGIC, val: uint64(0)}
			} else {
				tg, err := er.tagMan.GetAndPopulate(string(name))
				if err != nil {
					er.ackChan <- ackCommand{cmd: ERROR_TAG_MAGIC, val: uint64(0)}
				} else {
					er.ackChan <- ackCommand{cmd: CONFIRM_TAG_MAGIC, val: uint64(tg)}
				}
			}
			continue
		case INGESTER_STATE_MAGIC:
			// read length of buffer
			n, err = io.ReadFull(er.bIO, er.buff[0:4])
			if err != nil {
				return err
			}
			if n < 4 {
				return errFailedFullRead
			}
			length := binary.LittleEndian.Uint32(er.buff[0:4])
			if length > maxIngestStateSize {
				return ErrInvalidIngestStateHeader
			}
			stateBuff := make([]byte, length)
			n, err = io.ReadFull(er.bIO, stateBuff)
			if err != nil {
				return err
			}
			if n < int(length) {
				return errFailedFullRead
			}

			// Just send a confirmation
			er.ackChan <- ackCommand{cmd: CONFIRM_INGESTER_STATE_MAGIC}

			// Decode
			var state IngesterState
			if err = json.Unmarshal(stateBuff, &state); err != nil {
				// ignore it, it's just a state message
				continue
			}

			// store it for later retrieval
			er.igStateMtx.Lock()
			state.LastSeen = time.Now()
			er.igState = state
			er.igStateMtx.Unlock()

			// run callbacks
			for i := range er.stateCallbacks {
				er.stateCallbacks[i](state)
			}

			continue
		default: //we should probably bail out if we get desynced
			continue
		}
	}
	//read entry header worth as well as id (64bit)
	n, err = io.ReadFull(er.bIO, er.buff[:entry.ENTRY_HEADER_SIZE+8])
	if err != nil {
		return err
	}
	dataSize, hasEvs, err := ent.DecodeHeader(er.buff)
	if err != nil {
		return err
	}
	if dataSize > int(MAX_ENTRY_SIZE) {
		return ErrOversizedEntry
	}
	*sz = uint32(dataSize) //dataSize is a uint32 internally, so these casts are OK
	*id = entrySendID(binary.LittleEndian.Uint64(er.buff[entry.ENTRY_HEADER_SIZE:]))
	*evs = hasEvs
	return nil
}

// IngestOK waits for the ingester to send an INGEST_OK message and responds with
// the argument given. Any other command will make it return an error
func (er *EntryReader) IngestOK(ok bool) (err error) {
	var cmd []byte
	for {
		cmd, err = er.bIO.Peek(4)
		if err == bufio.ErrBufferFull {
			// we just don't have 4 bytes yet, sleep a little and try again
			time.Sleep(100 * time.Millisecond)
			continue
		} else if err != nil {
			// some other problem...
			return
		}
		break
	}

	switch IngestCommand(binary.LittleEndian.Uint32(cmd)) {
	case INGEST_OK_MAGIC:
		// discard the command since we already read it
		_, err = er.bIO.Discard(4)
		if err != nil {
			// very, very weird
			return err
		}

		// Now send response
		ac := ackCommand{cmd: CONFIRM_INGEST_OK_MAGIC}
		if ok {
			ac.val = 1
		}
		er.ackChan <- ac
	default:
		err = errors.New("Unknown message when looking for IngestOK, exiting")
	}
	return
}

// SetupConnection negotiations ingester API version and other information
// It should properly handle old ingesters, too
func (er *EntryReader) SetupConnection() (err error) {
	var n int
	er.igAPIVersion = MINIMUM_INGEST_OK_VERSION - 1 // default to assuming it's pretty old
	for {
		var cmd []byte
		cmd, err = er.bIO.Peek(4)
		if err == bufio.ErrBufferFull {
			// we just don't have 4 bytes yet, sleep a little and try again
			time.Sleep(50 * time.Millisecond)
			continue
		} else if err != nil {
			// some other problem...
			return
		}

		switch IngestCommand(binary.LittleEndian.Uint32(cmd)) {
		case FORCE_ACK_MAGIC:
			// discard the command since we already read it
			n, err = er.bIO.Discard(4)
			if err != nil {
				// very, very weird
				return err
			}
			if err := er.forceAck(); err != nil {
				return err
			}
		case ID_MAGIC:
			// discard the command since we already read it
			n, err = er.bIO.Discard(4)
			if err != nil {
				// very, very weird
				return err
			}

			// read name
			n, err = io.ReadFull(er.bIO, er.buff[0:4])
			if err != nil {
				return err
			}
			if n < 4 {
				return errFailedFullRead
			}
			length := binary.LittleEndian.Uint32(er.buff[0:4])
			name := make([]byte, length)
			n, err = io.ReadFull(er.bIO, name)
			if err != nil {
				return err
			}
			if n < int(length) {
				return errFailedFullRead
			}

			// read version
			n, err = io.ReadFull(er.bIO, er.buff[0:4])
			if err != nil {
				return err
			}
			if n < 4 {
				return errFailedFullRead
			}
			length = binary.LittleEndian.Uint32(er.buff[0:4])
			version := make([]byte, length)
			n, err = io.ReadFull(er.bIO, version)
			if err != nil {
				return err
			}
			if n < int(length) {
				return errFailedFullRead
			}

			// read uuid
			n, err = io.ReadFull(er.bIO, er.buff[0:4])
			if err != nil {
				return err
			}
			if n < 4 {
				return errFailedFullRead
			}
			length = binary.LittleEndian.Uint32(er.buff[0:4])
			id := make([]byte, length)
			n, err = io.ReadFull(er.bIO, id)
			if err != nil {
				return err
			}
			if n < int(length) {
				return errFailedFullRead
			}

			// respond
			er.ackChan <- ackCommand{cmd: CONFIRM_ID_MAGIC, val: uint64(0)}

			// set id values
			er.igName = string(name)
			er.igVersion = string(version)
			er.igUUID = string(id)
		case API_VER_MAGIC:
			// discard the command since we already read it
			n, err = er.bIO.Discard(4)
			if err != nil {
				// very, very weird
				return err
			}

			// read the version
			n, err = io.ReadFull(er.bIO, er.buff[0:2])
			if err != nil {
				return
			}
			if n < 2 {
				return errFailedFullRead
			}
			er.igAPIVersion = binary.LittleEndian.Uint16(er.buff[0:2])
			er.ackChan <- ackCommand{cmd: CONFIRM_API_VER_MAGIC, val: uint64(0)}
		default:
			// any other command means the ingester is no longer interested in identifying itself, so we're done
			return
		}
	}
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
	keepalivebuff := make([]byte, 32)
	var off int
	var err error
	var to bool

	//set the write buffer size
	if err = setWriteBuffer(er.conn, ACK_WRITER_BUFFER_SIZE); err != nil {
		er.routineCleanFail(err)
		return
	}

	tmr := time.NewTimer(keepAliveInterval)
	defer tmr.Stop()
	for {
		to = false
		select {
		case v, ok := <-er.ackChan:
			if !ok {
				return
			}
			if to, err = er.fillAndSendAckBuffer(buff, v, tmr.C); err != nil {
				er.routineCleanFail(err)
				return
			}
		case _ = <-tmr.C:
			to = true
		}
		//check if we had a timeout and need to send a keepalive
		if to {
			ac := ackCommand{cmd: PONG_MAGIC}
			if off, _, err = ac.encode(keepalivebuff); err != nil {
				er.routineCleanFail(err)
				return
			}
			if err = er.writeAll(keepalivebuff[:off]); err != nil {
				er.routineCleanFail(err)
				return
			}
			if err = er.bAckWriter.Flush(); err != nil {
				er.routineCleanFail(err)
				return
			}
		}
		tmr.Reset(keepAliveInterval)
	}
}

func (er *EntryReader) fillAndSendAckBuffer(b []byte, v ackCommand, toch <-chan time.Time) (to bool, err error) {
	var off int
	var n int
	var flush bool
	var ok bool
	//encode value into the buffer
	if off, flush, err = v.encode(b); err != nil {
		return
	} else if flush {
		if err = er.writeAll(b[:off]); err != nil {
			return
		}
		err = er.bAckWriter.Flush()
		return
	}

	//fire up a timer and start feeding
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
			if off == len(b) {
				break feedLoop
			} else if flush {
				to = true
				break feedLoop
			} else if len(er.ackChan) == 0 {
				break feedLoop
			}
		case _ = <-toch:
			to = true
			break feedLoop
		}
	}
	if off > 0 {
		if err = er.writeAll(b[:off]); err != nil {
			return
		}
		if err = er.bAckWriter.Flush(); err == nil {
			//clear the timeout if we got a good flush
			to = false
		}
	}
	return
}

func (er *EntryReader) SendThrottle(d time.Duration) error {
	if !er.started {
		return errAckRoutineClosed
	}
	er.ackChan <- ackCommand{cmd: THROTTLE_MAGIC, val: uint64(d)}
	return er.errState
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

// sends a command down the channel indicating that we should flush
func (er *EntryReader) forceAck() error {
	if !er.started {
		return errAckRoutineClosed
	}
	er.ackChan <- ackCommand{cmd: FORCE_ACK_MAGIC}
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
	case ERROR_TAG_MAGIC:
		return 4
	case CONFIRM_TAG_MAGIC:
		return 6
	case THROTTLE_MAGIC:
		return throttleEncodeSize
	case CONFIRM_ID_MAGIC:
		return 4
	case CONFIRM_API_VER_MAGIC:
		return 4
	case CONFIRM_INGEST_OK_MAGIC:
		return 4
	case CONFIRM_INGESTER_STATE_MAGIC:
		return 4
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
	case ERROR_TAG_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		n += 4
		flush = true
	case CONFIRM_TAG_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		binary.LittleEndian.PutUint64(b[4:], ac.val)
		n += confirmTagSize
		flush = true
	case CONFIRM_ID_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		n += 4
		flush = true
	case CONFIRM_API_VER_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		n += 4
		flush = true
	case CONFIRM_INGEST_OK_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		binary.LittleEndian.PutUint64(b[4:], ac.val)
		n += 12
		flush = true
	case CONFIRM_INGESTER_STATE_MAGIC:
		binary.LittleEndian.PutUint32(b, uint32(ac.cmd))
		n += 4
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
	case FORCE_ACK_MAGIC:
		ok = true
	case PONG_MAGIC:
		ok = true
	case ERROR_TAG_MAGIC:
		ok = true
	case CONFIRM_TAG_MAGIC:
		if _, err = io.ReadFull(rdr, val); err != nil {
			return
		}
		ac.val = binary.LittleEndian.Uint64(val)
		ok = true
	case CONFIRM_ID_MAGIC:
		ok = true
	case CONFIRM_API_VER_MAGIC:
		ok = true
	case CONFIRM_INGEST_OK_MAGIC:
		if _, err = io.ReadFull(rdr, val); err != nil {
			return
		}
		ac.val = binary.LittleEndian.Uint64(val)
		ok = true
	case CONFIRM_INGESTER_STATE_MAGIC:
		ok = true
	default:
		err = errUnknownCommand
	}
	return
}

func (ac *ackCommand) shouldFlush() bool {
	return ac.cmd == FORCE_ACK_MAGIC
}

func setReadBuffer(c interface{}, n int) error {
	switch v := c.(type) {
	case *net.IPConn:
		return v.SetReadBuffer(n)
	case *net.TCPConn:
		return v.SetReadBuffer(n)
	case *net.UDPConn:
		return v.SetReadBuffer(n)
	case *net.UnixConn:
		return v.SetReadBuffer(n)
	}
	//if we can't, we don't
	return nil
}

func setWriteBuffer(c interface{}, n int) error {
	switch v := c.(type) {
	case *net.IPConn:
		return v.SetWriteBuffer(n)
	case *net.TCPConn:
		return v.SetWriteBuffer(n)
	case *net.UDPConn:
		return v.SetWriteBuffer(n)
	case *net.UnixConn:
		return v.SetWriteBuffer(n)
	}
	//if we can't, we don't
	return nil
}
