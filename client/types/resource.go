/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"fmt"
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
}

// This is used for client->server resource sync operations, basically "I am a
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
func (m1 ResourceMetadata) Equal(m2 ResourceMetadata) bool {
	if m1.GroupACL == nil {
		m1.GroupACL = []int32{}
	}
	if m2.GroupACL == nil {
		m2.GroupACL = []int32{}
	}
	if m1.Hash == nil {
		m1.Hash = []byte{}
	}
	if m2.Hash == nil {
		m2.Hash = []byte{}
	}
	if m1.Labels == nil {
		m1.Labels = []string{}
	}
	if m2.Labels == nil {
		m2.Labels = []string{}
	}
	if m1.LastModified.Equal(m2.LastModified) {
		m2.LastModified = m1.LastModified
	}
	return reflect.DeepEqual(m1, m2)
}

func (m ResourceMetadata) String() string {
	return fmt.Sprintf("%s:%d", m.GUID, m.Domain)
}
