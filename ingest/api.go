/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

const (
	//MAJOR API VERSIONS should always be compatible, there just may be additional features
	API_VERSION_MAJOR uint32 = 0
	API_VERSION_MINOR uint32 = uint32(VERSION)
)

const (
	configurationBlockSize          uint32          = 1
	maxStreamConfigurationBlockSize uint32          = 1024 * 1024      //just a sanity check
	maxIngestStateSize              uint32          = 1024 * 1024      //size at which we start trimming the reporting-only config/metadata blocks
	maxIngestStateStupidSize        uint32          = 64 * 1024 * 1024 //size at which a state block is so absurd we assume something is broken and cut the connection
	CompressNone                    CompressionType = 0
	CompressSnappy                  CompressionType = 0x10
)

var (
	ErrInvalidBuffer            = errors.New("invalid buffer")
	ErrInvalidIngestStateHeader = errors.New("Invalid ingest state header")
	ErrOversizedConfigBlock     = errors.New("configuration block too large")
	ErrOversizedIngestState     = errors.New("ingester state block absurdly large")
	ErrEmptyConfigBlock         = errors.New("configuration block empty")
)

type CompressionType uint8

func PrintVersion(wtr io.Writer) {
	fmt.Fprintf(wtr, "API Version:\t%d.%d\n", API_VERSION_MAJOR, API_VERSION_MINOR)
}

// StreamConfiguration is a structure that can be sent back and
type StreamConfiguration struct {
	Compression CompressionType
}

func (c StreamConfiguration) Write(wtr io.Writer) (err error) {
	var n int
	buff := make([]byte, configurationBlockSize+4)
	binary.LittleEndian.PutUint32(buff, configurationBlockSize)
	if err = c.encode(buff[4:]); err != nil {
		return
	}
	if n, err = wtr.Write(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to write configuration block")
	}
	return
}

func (c *StreamConfiguration) Read(rdr io.Reader) (err error) {
	//read the block size
	var bsz uint32
	var n int
	if err = binary.Read(rdr, binary.LittleEndian, &bsz); err != nil {
		return
	}
	if bsz > maxStreamConfigurationBlockSize {
		err = ErrOversizedConfigBlock
		return
	} else if bsz == 0 {
		err = ErrEmptyConfigBlock
		return
	}
	buff := make([]byte, bsz)
	if n, err = rdr.Read(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to read configuration block")
		return
	}

	err = c.decode(buff)

	return
}

func (c StreamConfiguration) encode(buff []byte) (err error) {
	if len(buff) == 0 {
		err = ErrInvalidBuffer
		return
	}
	buff[0] = byte(c.Compression)
	return
}

func (c *StreamConfiguration) decode(buff []byte) (err error) {
	if len(buff) < 1 {
		err = ErrInvalidBuffer
		return
	}
	c.Compression = CompressionType(buff[0])

	err = c.validate()
	return
}

func (c *StreamConfiguration) validate() (err error) {
	if err = c.Compression.validate(); err != nil {
		return
	}

	return
}

func (ct CompressionType) validate() (err error) {
	switch ct {
	case CompressNone:
	case CompressSnappy:
	default:
		err = fmt.Errorf("Unknown compression id %x", ct)
	}
	return
}

func ParseCompression(v string) (ct CompressionType, err error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ``:
	case `none`:
	case `snappy`:
		ct = CompressSnappy
	default:
		err = fmt.Errorf("Unknown compression type %q", v)
	}
	return
}

type IngesterState struct {
	UUID          string
	Name          string
	Version       string
	Label         string
	IP            net.IP        //child IP, won't be populated unless in child
	Hostname      string        // whatever the ingester thinks its hostname is
	Entries       uint64        // How many entries the ingester has written
	Size          uint64        // How many bytes the ingester has written
	Uptime        time.Duration // Nanoseconds since the ingest muxer was initialized
	Tags          []string      // The tags registered with the ingester
	CacheState    string
	CacheSize     uint64
	LastSeen      time.Time
	Children      map[string]IngesterState
	Configuration jsontext.Value `json:",omitempty"`
	Metadata      jsontext.Value `json:",omitempty"`
}

type writeCounter struct {
	bts int
}

func (wc *writeCounter) Write(b []byte) (n int, err error) {
	n = len(b)
	wc.bts += n
	return
}

func (s *IngesterState) EncodedSize() (uint32, error) {
	var wc writeCounter
	if err := json.MarshalWrite(&wc, s, jsoncompat.Options); err != nil {
		return 0, err
	}
	return uint32(wc.bts), nil
}

func trimChildConfigs(children map[string]IngesterState, depth int) {
	for k, v := range children {
		if depth <= 0 {
			// just zap all children
			v.Children = nil
		}
		v.Configuration = nil
		v.Metadata = nil
		if len(v.Children) > 0 {
			trimChildConfigs(v.Children, depth-1)
		}
		children[k] = v
	}
}

func (s *IngesterState) trimChildConfigs() {
	trimChildConfigs(s.Children, 8) //anything deeper than 8, just nuke em
}

// trimChildren caps the reported children at maxCount, discarding an arbitrary subset of the extras.
func (s *IngesterState) trimChildren(maxCount int) {
	if len(s.Children) > maxCount {
		var x int
		for k := range s.Children {
			x++
			if x > maxCount {
				delete(s.Children, k)
			}
		}
	}
}

func (s *IngesterState) Write(wtr io.Writer) (err error) {
	// First, encode to JSON
	var data []byte
	if data, err = json.Marshal(s, jsoncompat.Options); err != nil {
		return err
	} else if len(data) > int(maxIngestStateSize) || len(data) == 0 {
		return ErrInvalidIngestStateHeader
	}

	// Now send the size
	var n int
	buff := make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, uint32(len(data)))
	if n, err = wtr.Write(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to write ingest state size block")
		return
	}

	// and write the JSON
	if n, err = wtr.Write(data); err != nil {
		return
	} else if n != len(data) {
		err = errors.New("Failed to write encoded ingest state")
	}

	return
}

func (s *IngesterState) Read(rdr io.Reader) (err error) {
	// First read out the size (32-bit integer)
	var bsz uint32
	if err = binary.Read(rdr, binary.LittleEndian, &bsz); err != nil {
		return
	}
	if bsz == 0 {
		err = ErrInvalidIngestStateHeader
		return
	} else if bsz > maxIngestStateStupidSize {
		// a state block this large is absurd, refuse it outright and let the
		// caller tear down the connection
		err = ErrOversizedIngestState
		return
	} else if bsz > maxIngestStateSize {
		// downstream reporter is trying to send us a config block that is too large.
		// we don't want to kick the connection but we don't want this config report either
		// just read and discard the bytes without updating our internal config or metadata
		// io.CopyN streams through a small fixed buffer so we never hold the whole block in memory
		if _, err = io.CopyN(io.Discard, rdr, int64(bsz)); err != nil {
			err = fmt.Errorf("failed to discard oversized ingest state: %w", err)
		}
		return
	}

	// We are informed that the config block is within our tolerance to consume and report
	// Now read that much data off the reader
	buff := make([]byte, bsz)
	if _, err = io.ReadFull(rdr, buff); err != nil {
		err = fmt.Errorf("failed to read ingest state: %w", err)
		return
	}

	// Decode the JSON
	if err = json.Unmarshal(buff, s, jsoncompat.Options); err != nil {
		return
	}

	return
}

// Copy creates a deep copy of the ingester state, this is important when handing the data type off to a gob encoder
// if the server updates the ingester state when it is attempting to encode a state blob we could get a race
// where the internal map is updated while we are attempting to encode it, this would cause fault
// Copy returns a copy of the state that is safe to hand to another goroutine.
//
// Children, Tags, and IP are duplicated.  Configuration and Metadata deliberately
// keep sharing their backing arrays with the original.  Those are opaque blobs the
// remote ingester controls, bounded only by maxIngestStateSize, and duplicating
// them here would put an unbounded per ingester cost on the indexer's stats poll,
// which copies this state on every tick.  That sharing is only sound so long as
// they are replaced whole rather than written in place, so never index assign into
// a stored state's Configuration or Metadata, build a new slice and assign it.
func (s IngesterState) Copy() (r IngesterState) {
	r = s
	//copy the map
	r.Children = make(map[string]IngesterState, len(s.Children))
	for k, v := range s.Children {
		r.Children[k] = v.Copy()
	}
	//copy the slice headers a caller could reasonably sort or rewrite in place.
	//string contents are immutable so this stays cheap, it is headers only.
	if s.Tags != nil {
		r.Tags = make([]string, len(s.Tags))
		copy(r.Tags, s.Tags)
	}
	if s.IP != nil {
		r.IP = make(net.IP, len(s.IP))
		copy(r.IP, s.IP)
	}
	return
}

type es []string

func (e es) MarshalJSON() ([]byte, error) {
	if len(e) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(e), jsoncompat.Options)
}

type mis struct {
	mp map[string]IngesterState
}

func (m mis) MarshalJSON() ([]byte, error) {
	if len(m.mp) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m.mp, jsoncompat.Options)
}

func (s IngesterState) MarshalJSON() ([]byte, error) {
	x := struct {
		UUID          string
		Name          string
		Version       string
		Label         string
		IP            net.IP
		Hostname      string
		Entries       uint64
		Size          uint64
		Uptime        time.Duration
		Tags          es
		CacheState    string
		CacheSize     uint64
		LastSeen      time.Time
		Children      mis
		Configuration jsontext.Value `json:",omitempty"`
		Metadata      jsontext.Value `json:",omitempty"`
	}{
		UUID:          s.UUID,
		Name:          s.Name,
		Version:       s.Version,
		Label:         s.Label,
		IP:            s.IP,
		Hostname:      s.Hostname,
		Entries:       s.Entries,
		Size:          s.Size,
		Uptime:        s.Uptime,
		Tags:          es(s.Tags),
		CacheState:    s.CacheState,
		CacheSize:     s.CacheSize,
		LastSeen:      s.LastSeen,
		Children:      mis{mp: s.Children},
		Configuration: s.Configuration,
		Metadata:      s.Metadata,
	}
	return json.Marshal(x, jsoncompat.Options)
}
