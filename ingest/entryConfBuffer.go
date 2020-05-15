/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	//This MUST be > 1 or the universe explodes
	//no matter what a user requests, this is the maximum
	//basically a sanity check
	ABSOLUTE_MAX_UNCONFIRMED_WRITES int = 0xffff
)

var (
	errEmptyList       = errors.New("Empty list")
	errEmptyConfBuff   = errors.New("Empty confirmation buffer")
	errCorruptConfBuff = errors.New("entry confirmationg buff is corrupt")
	errEntryNotFound   = errors.New("EntryID not found")
	errFullBuffer      = errors.New("Buffer is full")
)

type entryConfirmation struct {
	EntryID entrySendID
	Ent     *entry.Entry
}

// This structure and its methods is NOT thread safe, the caller
// should ensure that all accesses are syncronous
type entryConfBuffer struct {
	buff     [](*entryConfirmation)
	capacity int
	head     int
	count    int
}

func newEntryConfirmationBuffer(unconfirmedBufferSize int) (entryConfBuffer, error) {
	//if its too big we just size it down
	if unconfirmedBufferSize > ABSOLUTE_MAX_UNCONFIRMED_WRITES {
		unconfirmedBufferSize = ABSOLUTE_MAX_UNCONFIRMED_WRITES
	}
	buff := make([](*entryConfirmation), unconfirmedBufferSize)
	return entryConfBuffer{buff, unconfirmedBufferSize, 0, 0}, nil
}

func (ecb *entryConfBuffer) outstandingEntries() []*entry.Entry {
	if len(ecb.buff) == 0 {
		return nil
	}
	var ents []*entry.Entry
	idx := ecb.head
	for i := 0; i < ecb.count; i++ {
		if idx == len(ecb.buff) {
			idx = 0
		}
		if ecb.buff[idx] == nil {
			break
		}
		ents = append(ents, ecb.buff[idx].Ent)
		idx++
	}
	return ents
}

func (ecb *entryConfBuffer) IsHead(id entrySendID) (bool, error) {
	if ecb.count <= 0 {
		return false, errEmptyList
	}
	if ecb.buff[ecb.head].EntryID == id {
		return true, nil
	}
	return false, nil
}

func (ecb *entryConfBuffer) Full() bool {
	if ecb.count >= (ecb.capacity - 1) {
		return true
	}
	return false
}

func (ecb *entryConfBuffer) Count() int {
	return ecb.count
}

func (ecb *entryConfBuffer) Size() int {
	return ecb.capacity
}

//Free returns how many slots are available
func (ecb *entryConfBuffer) Free() int {
	return ecb.capacity - ecb.count

}

// A confirmation removes the ID from our queue
func (ecb *entryConfBuffer) Confirm(id entrySendID) error {
	if ecb.count <= 0 {
		return errEmptyConfBuff
	}
	//check the head first as that is what SHOULD be hitting
	ec := ecb.buff[ecb.head]
	if ec == nil {
		return errCorruptConfBuff
	}
	if ec.EntryID != id {
		return ecb.popUnalligned(id)
	}
	_, err := ecb.popHead()
	return err
}

// typically used when we need to resend something
func (ecb *entryConfBuffer) GetEntry(id entrySendID) (*entry.Entry, error) {
	//walk up the list and find the entry associated with the ID
	for i := ecb.head; i < ecb.count; i++ {
		//its a circular list so we need to check for runovers
		if i == ecb.capacity {
			i = 0
		}
		if ecb.buff[i] == nil {
			return nil, errCorruptConfBuff
		}
		if ecb.buff[i].EntryID == id {
			return ecb.buff[i].Ent, nil
		}
	}
	return nil, errEntryNotFound
}

func (ecb *entryConfBuffer) popHead() (*entry.Entry, error) {
	var ent *entry.Entry
	if ecb.buff[ecb.head] == nil {
		return nil, errors.New("head is nil")
	}
	ent = ecb.buff[ecb.head].Ent
	ecb.buff[ecb.head] = nil
	//adjust index and count
	ecb.head++
	ecb.count--
	//wrap if needed
	if ecb.head == ecb.capacity {
		ecb.head = 0
	}
	return ent, nil
}

// this can be extremely expensive, but should only be happening on
// error conditions. Its job is to go find an ID, remove it from the
// list and shift all items forward to fill the gap
func (ecb *entryConfBuffer) popUnalligned(id entrySendID) error {
	var curr, next int
	//simple sanity check incase we are popping the head
	if ecb.buff[ecb.head] != nil && ecb.buff[ecb.head].EntryID == id {
		_, err := ecb.popHead()
		return err
	}
	//not the head, so go do the hard work
	for i := ecb.head; i < ecb.count; i++ {
		if i == ecb.capacity {
			i = 0
		}
		if ecb.buff[i] == nil {
			return errCorruptConfBuff
		}
		//found the ID, so remove it and shift forward
		//if this hits we ARE going to return
		if ecb.buff[i].EntryID == id {
			//remove the ID from the list
			for ; i < ecb.count; i++ {
				if i == ecb.capacity {
					i = 0
				}
				curr = i
				next = i + 1
				if next == ecb.capacity {
					next = 0
				}
				ecb.buff[curr] = ecb.buff[next]
			}
			//at this point everything is shifted forward and
			//next points to the last item
			ecb.buff[next] = nil
			//the first time should never hit here, so we can
			//just decriment count and don't need to shift head
			ecb.count--

			return nil
		}
	}

	return errEntryNotFound
}

func (ecb *entryConfBuffer) Add(ec *entryConfirmation) error {
	var tail int
	if (ecb.count + 1) >= ecb.capacity {
		return errFullBuffer
	}
	//calculate the location to add the entry
	tail = ((ecb.head + ecb.count) % ecb.capacity)
	ecb.buff[tail] = ec
	ecb.count++
	return nil
}
