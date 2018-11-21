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
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gravwell/ingest/entry"
)

const (
	ACK_SIZE int = 12 //ackmagic + entrySendID
	//READ_ENTRY_HEADER_SIZE should be 46 bytes
	//34 + 4 + 4 + 8 (magic, data len, entry ID)
	READ_ENTRY_HEADER_SIZE int = entry.ENTRY_HEADER_SIZE + 12
	//TODO: We should make this configurable by configuration
	MAX_ENTRY_SIZE              int           = 128 * 1024 * 1024
	WRITE_BUFFER_SIZE           int           = 1024 * 1024
	MAX_WRITE_ERROR             int           = 4
	BUFFERED_ACK_READER_SIZE    int           = ACK_SIZE * MAX_UNCONFIRMED_COUNT
	CLOSING_SERVICE_ACK_TIMEOUT time.Duration = time.Second

	MAX_UNCONFIRMED_COUNT int = 1024 * 4

	MINIMUM_TAG_RENEGOTIATE_VERSION uint16 = 0x2 // minimum server version to renegotiate tags

	maxThrottleDur time.Duration = 5 * time.Second

	flushTimeout time.Duration = 10 * time.Second
)

const (
	//ingester commands
	INVALID_MAGIC       IngestCommand = 0x00000000
	NEW_ENTRY_MAGIC     IngestCommand = 0xC7C95ACB
	FORCE_ACK_MAGIC     IngestCommand = 0x1ADF7350
	CONFIRM_ENTRY_MAGIC IngestCommand = 0xF6E0307E
	THROTTLE_MAGIC      IngestCommand = 0xBDEACC1E
	PING_MAGIC          IngestCommand = 0x88770001
	PONG_MAGIC          IngestCommand = 0x88770008
	TAG_MAGIC           IngestCommand = 0x18675300
	CONFIRM_TAG_MAGIC   IngestCommand = 0x18675301
	ERROR_TAG_MAGIC     IngestCommand = 0x18675302
)

type IngestCommand uint32
type entrySendID uint64

type EntryWriter struct {
	conn          Conn
	bIO           *bufio.Writer
	bAckReader    *bufio.Reader
	errCount      uint32
	mtx           *sync.Mutex
	ecb           entryConfBuffer
	hot           bool
	buff          []byte
	id            entrySendID
	ackTimeout    time.Duration
	serverVersion uint16
}

func NewEntryWriter(conn net.Conn) (*EntryWriter, error) {
	ewc := EntryReaderWriterConfig{
		Conn:                  NewUnthrottledConn(conn),
		OutstandingEntryCount: MAX_UNCONFIRMED_COUNT,
		BufferSize:            WRITE_BUFFER_SIZE,
		Timeout:               CLOSING_SERVICE_ACK_TIMEOUT,
	}
	return NewEntryWriterEx(ewc)
}

type EntryReaderWriterConfig struct {
	Conn                  net.Conn
	OutstandingEntryCount int
	BufferSize            int
	Timeout               time.Duration
	TagMan                TagManager
}

func NewEntryWriterEx(cfg EntryReaderWriterConfig) (*EntryWriter, error) {
	ecb, err := newEntryConfirmationBuffer(cfg.OutstandingEntryCount)
	if err != nil {
		return nil, err
	}

	return &EntryWriter{
		conn:       NewUnthrottledConn(cfg.Conn),
		bIO:        bufio.NewWriterSize(cfg.Conn, cfg.BufferSize),
		bAckReader: bufio.NewReaderSize(cfg.Conn, cfg.OutstandingEntryCount*ACK_SIZE),
		mtx:        &sync.Mutex{},
		ecb:        ecb,
		hot:        true,
		buff:       make([]byte, READ_ENTRY_HEADER_SIZE),
		id:         1,
		ackTimeout: cfg.Timeout,
	}, nil
}

func (ew *EntryWriter) OverrideAckTimeout(t time.Duration) error {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()
	ew.ackTimeout = t
	if t <= 0 {
		return errors.New("invalid duration")
	}
	return nil
}

func (ew *EntryWriter) SetConn(c Conn) {
	ew.mtx.Lock()
	ew.conn = c
	ew.bIO.Reset(c)
	ew.mtx.Unlock()
}

func (ew *EntryWriter) Close() (err error) {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()

	if err = ew.forceAckNoLock(); err == nil {
		if err = ew.conn.SetReadTimeout(ew.ackTimeout); err != nil {
			err = ew.conn.Close()
			ew.hot = false
			return
		}
		//read acks is a liberal implementation which will pull any available
		//acks from the read buffer.  we don't care if we get an error here
		//because this is largely used when trying to refire a connection
		err = ew.readAcks(true)
	}

	ew.hot = false
	ew.conn.Close()
	return
}

func (ew *EntryWriter) ForceAck() error {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()
	return ew.forceAckNoLock()
}

func (ew *EntryWriter) outstandingEntries() []*entry.Entry {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()
	return ew.ecb.outstandingEntries()
}

func (ew *EntryWriter) throwAckSync() error {
	//send the buffer and force it out
	if err := ew.writeAll(FORCE_ACK_MAGIC.Buff()); err != nil {
		return err
	}
	return ew.flush()
}

// Ping is essentially a force ack, we send a PING command, which will cause
// the server to flush all acks and a PONG command.  We read until we get the PONG
func (ew *EntryWriter) Ping() (err error) {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()
	//send the buffer and force it out
	if err = ew.writeAll(PING_MAGIC.Buff()); err != nil {
		return
	}
	if err = ew.flush(); err != nil {
		return
	}
	//start servicing responses until we get an ACK
	err = ew.readCommandsUntil(PONG_MAGIC)
	return
}

// forceAckNoLock sends a signal to the ingester that we want to force out
// and ACK of all outstanding entries.  This is primarily used when
// closing the connection to ensure that all the entries actually
// made it to the ingester. The caller MUST hold the lock
func (ew *EntryWriter) forceAckNoLock() error {
	if err := ew.throwAckSync(); err != nil {
		return err
	}
	//begin servicing acks with blocking and a read deadline
	for ew.ecb.Count() > 0 {
		if err := ew.conn.SetReadTimeout(ew.ackTimeout); err != nil {
			return err
		}
		if err := ew.serviceAcks(true); err != nil {
			ew.conn.ClearReadTimeout()
			return err
		}
		ew.conn.ClearReadTimeout()
	}
	if ew.ecb.Count() > 0 {
		return fmt.Errorf("Failed to confirm %d entries", ew.ecb.Count())
	}
	return nil
}

// Write expects to have exclusive control over the entry and all
// its buffers from the period of write and forever after.
// This is because it needs to be able to resend the entry if it
// fails to confirm.  If a buffer is re-used and the entry fails
// to confirm we will send the new modified buffer which may not
// have the original data.
func (ew *EntryWriter) Write(ent *entry.Entry) error {
	return ew.writeFlush(ent, false)
}

func (ew *EntryWriter) WriteSync(ent *entry.Entry) error {
	return ew.writeFlush(ent, true)
}

func (ew *EntryWriter) writeFlush(ent *entry.Entry, flush bool) error {
	var err error
	var blocking bool

	ew.mtx.Lock()
	if ew.ecb.Full() {
		blocking = true
	} else {
		blocking = false
	}

	//check if any acks can be serviced
	if err = ew.serviceAcks(blocking); err != nil {
		ew.mtx.Unlock()
		return err
	}

	_, err = ew.writeEntry(ent, flush)
	ew.mtx.Unlock()
	return err
}

// OpenSlots informs the caller how many slots are available before
// we must service acks.  This is used for mostly in a multiplexing
// system where we want to know how much we can write before we need
// to service acks and move on.
func (ew *EntryWriter) OpenSlots(ent *entry.Entry) int {
	ew.mtx.Lock()
	r := ew.ecb.Free()
	ew.mtx.Unlock()
	return r
}

// WriteWithHint behaves exactly like Write but also returns a bool
// which indicates whether or not the a flush was required.  This
// function method is primarily used when muxing across multiple
// indexers, so the muxer knows when to transition to the next indexer
func (ew *EntryWriter) WriteWithHint(ent *entry.Entry) (bool, error) {
	var err error
	var blocking bool

	ew.mtx.Lock()
	defer ew.mtx.Unlock()
	if ew.ecb.Full() {
		blocking = true
	} else {
		blocking = false
	}

	//check if any acks can be serviced
	if err = ew.serviceAcks(blocking); err != nil {
		return false, err
	}
	return ew.writeEntry(ent, true)
}

// WriteBatch takes a slice of entries and writes them,
// this function is useful in multithreaded environments where
// we want to lessen the impact of hits on a channel by threads
func (ew *EntryWriter) WriteBatch(ents [](*entry.Entry)) error {
	var err error

	ew.mtx.Lock()
	defer ew.mtx.Unlock()

	for i := range ents {
		if _, err = ew.writeEntry(ents[i], false); err != nil {
			return err
		}
	}

	return nil
}

func (ew *EntryWriter) writeEntry(ent *entry.Entry, flush bool) (bool, error) {
	var flushed bool
	var err error
	//if our conf buffer is full force an ack service
	if ew.ecb.Full() {
		if err := ew.flush(); err != nil {
			return false, err
		}
		if err := ew.serviceAcks(true); err != nil {
			return false, err
		}
	}

	//throw the magic
	binary.LittleEndian.PutUint32(ew.buff, uint32(NEW_ENTRY_MAGIC))

	//build out the header with size
	if err = ent.EncodeHeader(ew.buff[4 : entry.ENTRY_HEADER_SIZE+4]); err != nil {
		return false, err
	}
	binary.LittleEndian.PutUint64(ew.buff[entry.ENTRY_HEADER_SIZE+4:], uint64(ew.id))
	//throw it and flush it
	if err = ew.writeAll(ew.buff); err != nil {
		return false, err
	}
	//only flush if we need to
	if len(ent.Data) > ew.bIO.Available() {
		flushed = true
		if err = ew.flush(); err != nil {
			return false, err
		}
	}
	//throw the actual data portion and flush it
	if err = ew.writeAll(ent.Data); err != nil {
		return false, err
	}
	if flush {
		flushed = flush
		if err = ew.flush(); err != nil {
			return false, err
		}
	}
	if err = ew.ecb.Add(&entryConfirmation{ew.id, ent}); err != nil {
		return false, err
	}
	ew.id++
	return flushed, nil
}

func (ew *EntryWriter) writeAll(b []byte) error {
	var (
		err   error
		n     int
		total int
		tgt   = len(b)
	)
	total = 0
	for total < tgt {
		n, err = ew.bIO.Write(b[total:tgt])
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("Failed to write bytes")
		}
		total += n
		if total == tgt {
			break
		}
		//if only a partial write occurred that means we need to flush
		if err = ew.flush(); err != nil {
			return err
		}
	}
	return nil
}

//flush attempts to push our buffer to the wire with a timeout
//if the timeout expires we attempt to service acks and go back to attempting to flush
func (ew *EntryWriter) flush() (err error) {
	//set the write timeout
	if err = ew.conn.SetWriteTimeout(flushTimeout); err != nil {
		return
	}

	//issue the flush with timeout
	if err = ew.bIO.Flush(); err == nil {
		err = ew.conn.ClearWriteTimeout()
	}
	return
}

func (ew *EntryWriter) NegotiateTag(name string) (tg entry.EntryTag, err error) {
	ew.mtx.Lock()
	defer ew.mtx.Unlock()

	if ew.serverVersion < MINIMUM_TAG_RENEGOTIATE_VERSION {
		err = fmt.Errorf("Server version %v does not meet minimum version %v", ew.serverVersion, MINIMUM_TAG_RENEGOTIATE_VERSION)
		return
	}

	// First attempt to sync
	err = ew.forceAckNoLock()
	if err != nil {
		return
	}

	// Send tag magic
	//send the buffer and force it out
	if err = ew.writeAll(TAG_MAGIC.Buff()); err != nil {
		return
	}

	// Send length of string + string
	b := make([]byte, 4+len(name))
	binary.LittleEndian.PutUint32(b, uint32(len(name)))
	copy(b[4:], name)
	if err = ew.writeAll(b); err != nil {
		return
	}

	if err = ew.flush(); err != nil {
		return
	}

	// Read back an ackCommand
	var ac ackCommand
	var ok bool
	if err = ew.conn.SetReadTimeout(time.Second); err != nil {
		return
	}
	if ok, err = ac.decode(ew.bAckReader, true); err != nil {
		return
	}
	if !ok {
		err = errors.New("couldn't figure out ackCommand")
		return
	}

	switch ac.cmd {
	case CONFIRM_TAG_MAGIC:
		tg = entry.EntryTag(ac.val)
	case ERROR_TAG_MAGIC:
		err = errors.New("Failed to negotiate tag")
	default:
		err = fmt.Errorf("Unexpected response to tag negotiation request: %#v", ac)
	}

	return
}

// Ack will block waiting for at least one ack to free up a slot for sending
func (ew *EntryWriter) Ack() error {
	ew.mtx.Lock()
	//ensure there are outstanding acks
	if ew.ecb.Count() == 0 {
		ew.mtx.Unlock()
		return nil
	}
	err := ew.serviceAcks(true)
	ew.mtx.Unlock()
	return err
}

// serviceAcks MUST be called with the parent holding the mutex
func (ew *EntryWriter) serviceAcks(blocking bool) error {
	//only flush if we are blocking
	if blocking && ew.bIO.Buffered() > 0 {
		if err := ew.flush(); err != nil {
			return err
		}
	}
	//attempt to read acks
	if err := ew.readAcks(blocking); err != nil {
		return err
	}
	if ew.ecb.Full() {
		//if we attempted to read and we are full, force a sync, something is wrong
		if err := ew.throwAckSync(); err != nil {
			return err
		}
		return ew.readAcks(true)
	}
	return nil
}

//readAcks pulls out all of the acks in the ackBuffer and services them
func (ew *EntryWriter) readAcks(blocking bool) (err error) {
	var ac ackCommand
	var ok bool
	var dur time.Duration

	for ew.ecb.Count() > 0 {
		if ok, err = ac.decode(ew.bAckReader, blocking); err != nil {
			return
		}
		if !ok {
			break
		}
		blocking = false
		switch ac.cmd {
		case CONFIRM_ENTRY_MAGIC:
			//check if the ID is the head, if not pop the head and resend
			//TODO: if we get an ID we don't know about we just ignore it
			//      is this the best course of action?
			if err = ew.ecb.Confirm(entrySendID(ac.val)); err != nil {
				if err != errEntryNotFound {
					return
				}
			}
		case THROTTLE_MAGIC:
			if dur = time.Duration(ac.val); dur > maxThrottleDur || dur < 0 {
				dur = maxThrottleDur
			}
			if err = ew.throttle(dur); err != nil {
				return
			}
		}
	}
	return
}

// readCommandsUntil pulls out all of the responses and services them,
// we block until we hit the command we want
func (ew *EntryWriter) readCommandsUntil(cmd IngestCommand) (err error) {
	var ac ackCommand
	var ok bool
	var dur time.Duration

	for ew.ecb.Count() > 0 {
		if ok, err = ac.decode(ew.bAckReader, true); err != nil {
			return
		} else if !ok {
			err = errFailedToReadCommand
			return
		}
		switch ac.cmd {
		case CONFIRM_ENTRY_MAGIC:
			//check if the ID is the head, if not pop the head and resend
			//TODO: if we get an ID we don't know about we just ignore it
			//      is this the best course of action?
			if err = ew.ecb.Confirm(entrySendID(ac.val)); err != nil {
				if err != errEntryNotFound {
					return
				}
			}
		case THROTTLE_MAGIC:
			if dur = time.Duration(ac.val); dur > maxThrottleDur || dur < 0 {
				dur = maxThrottleDur
			}
			if err = ew.throttle(dur); err != nil {
				return
			}
		}
		if ac.cmd == cmd {
			break
		}
	}
	return
}

func (ew *EntryWriter) throttle(dur time.Duration) (err error) {
	//check if we were asked to throttle
	if dur > 0 {
		//set the read deadline, and wait for a byte
		if err = ew.conn.SetReadTimeout(dur); err != nil {
			return
		}
		if _, err = ew.bAckReader.ReadByte(); err != nil {
			return
		} else if err = ew.bAckReader.UnreadByte(); err != nil {
			return
		}
		if err = ew.conn.ClearReadTimeout(); err != nil {
			return
		}
	}
	return
}

func (ew EntryWriter) OptimalBatchWriteSize() int {
	return ew.ecb.Size()
}

func (ic IngestCommand) String() string {
	switch ic {
	case NEW_ENTRY_MAGIC:
		return `NEW`
	case FORCE_ACK_MAGIC:
		return `FORCE ACK`
	case CONFIRM_ENTRY_MAGIC:
		return `CONFIRM ENTRY`
	case THROTTLE_MAGIC:
		return `THROTTLE`
	case INVALID_MAGIC:
		return `INVALID`
	case PING_MAGIC:
		return `PING`
	case PONG_MAGIC:
		return `PONG`
	case TAG_MAGIC:
		return `TAG`
	case ERROR_TAG_MAGIC:
		return `TAG_ERROR`
	case CONFIRM_TAG_MAGIC:
		return `TAG_CONFIRM`
		//case _MAGIC: return ``
	}
	return `UNKNOWN`
}

func (ic IngestCommand) Buff() (b []byte) {
	b = make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(ic))
	return
}

func getCommand(b []byte) IngestCommand {
	if len(b) < 4 { //less than uint32
		return INVALID_MAGIC
	}
	return IngestCommand(binary.LittleEndian.Uint32(b))
}
