/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"time"
)

type ResourceMetadata struct {
	UID           int32     // owner
	GUID          string    // unique ID for this resource
	Domain        int16     // The webserver domain of this resource. Only webservers in this domain can access it.
	LastModified  time.Time // time resource was modified, including metadata
	VersionNumber int       // resource version #, increment at each Write
	GroupACL      []int32   // GIDs which can access the resource
	Global        bool      // if Global is set, any user can read the resource.
	ResourceName  string
	Description   string
	Size          uint64
	Hash          []byte
	Synced        bool     // Set to true if this version is on the datastore, false otherwise
	Labels        []string // the backend doesn't really care about these, it's the GUI's problem
}

type ResourceUpdate struct {
	Metadata ResourceMetadata
	Data     []byte
	rdr      io.ReadCloser //do not export this, gob can't handle the type
}

// ResourceList is used for client->server resource sync operations, basically "I am a
// webserver from domain <Domain>, here's what I have (<List>), delete anything
// in my domain that's not on the list".
type ResourceList struct {
	List   []ResourceMetadata
	Domain int16
}

type ResourceContentType struct {
	ContentType string
	Body        []byte
}

// Equal returns true of both ResourceMetadata objects are identical.
func (m ResourceMetadata) Equal(m2 ResourceMetadata) bool {
	if m.GroupACL == nil {
		m.GroupACL = []int32{}
	}
	if m2.GroupACL == nil {
		m2.GroupACL = []int32{}
	}
	if m.Hash == nil {
		m.Hash = []byte{}
	}
	if m2.Hash == nil {
		m2.Hash = []byte{}
	}
	if m.Labels == nil {
		m.Labels = []string{}
	}
	if m2.Labels == nil {
		m2.Labels = []string{}
	}
	if m.LastModified.Equal(m2.LastModified) {
		m2.LastModified = m.LastModified
	}
	return reflect.DeepEqual(m, m2)
}

func (m ResourceMetadata) String() string {
	return fmt.Sprintf("%s:%d", m.GUID, m.Domain)
}

// Bytes returns a byte slice no matter what the underlying storage is
// if the ResourceUpdate is using a readCloser then it performs a complete read and
// returns a byte slice.  If the reader points to a large resource this may require significant resources
func (ru *ResourceUpdate) Bytes() (b []byte) {
	if ru.Data != nil {
		b = ru.Data
	} else {
		bb := bytes.NewBuffer(nil)
		io.Copy(bb, ru.rdr)
		b = bb.Bytes()
	}
	return
}

// Stream generates a io.Reader from either the underlying reader or the Data byte slice
func (ru *ResourceUpdate) Stream() io.Reader {
	if ru.rdr != nil {
		return ru.rdr
	}
	return bytes.NewBuffer(ru.Data)
}

// SetStream will set the resource update to use a read closer instead of static bytes
// we do not export the ReadCloser because gob can't handle it
func (ru *ResourceUpdate) SetStream(rc io.ReadCloser) {
	if ru != nil {
		ru.Data = nil
		ru.rdr = rc
	}
}

// Close is a safe method to make sure that ReadClosers and Byte Buffers are wiped out
func (ru *ResourceUpdate) Close() {
	if ru != nil {
		if ru.rdr != nil {
			ru.rdr.Close()
		}
		if ru.Data != nil {
			ru.Data = nil
		}
	}
}
