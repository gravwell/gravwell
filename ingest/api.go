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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/crewjam/rfc5424"
)

const (
	//MAJOR API VERSIONS should always be compatible, there just may be additional features
	API_VERSION_MAJOR uint32 = 0
	API_VERSION_MINOR uint32 = 6
)

const (
	configurationBlockSize          uint32          = 1
	maxStreamConfigurationBlockSize uint32          = 1024 * 1024 //just a sanity check
	maxIngestStateSize              uint32          = 1024 * 1024
	CompressNone                    CompressionType = 0
	CompressSnappy                  CompressionType = 0x10
)

var (
	ErrInvalidBuffer            = errors.New("invalid buffer")
	ErrInvalidIngestStateHeader = errors.New("Invalid ingest state header")
	ErrInvalidConfigBlock       = errors.New("Invalid configuration block size")
)

type CompressionType uint8

func PrintVersion(wtr io.Writer) {
	fmt.Fprintf(wtr, "API Version:\t%d.%d\n", API_VERSION_MAJOR, API_VERSION_MINOR)
}

type Logger interface {
	Infof(string, ...interface{}) error
	Warnf(string, ...interface{}) error
	Errorf(string, ...interface{}) error
	Info(string, ...rfc5424.SDParam) error
	Warn(string, ...rfc5424.SDParam) error
	Error(string, ...rfc5424.SDParam) error
	InfofWithDepth(int, string, ...interface{}) error
	WarnfWithDepth(int, string, ...interface{}) error
	ErrorfWithDepth(int, string, ...interface{}) error
	InfoWithDepth(int, string, ...rfc5424.SDParam) error
	WarnWithDepth(int, string, ...rfc5424.SDParam) error
	ErrorWithDepth(int, string, ...rfc5424.SDParam) error
	Hostname() string
	Appname() string
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
	if bsz > maxStreamConfigurationBlockSize || bsz == 0 {
		err = ErrInvalidConfigBlock
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
	IP            net.IP //child IP, won't be populated unless in child
	Entries       uint64
	CacheState    string
	CacheSize     uint64
	Children      map[string]IngesterState
	Configuration json.RawMessage `json:",omitempty"`
	Metadata      json.RawMessage `json:",omitempty"`
}

func (s *IngesterState) Write(wtr io.Writer) (err error) {
	// First, encode to JSON
	var data []byte
	if data, err = json.Marshal(s); err != nil {
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
	var n int
	if err = binary.Read(rdr, binary.LittleEndian, &bsz); err != nil {
		return
	}
	if bsz > maxIngestStateSize || bsz == 0 {
		err = ErrInvalidIngestStateHeader
		return
	}

	// Now read that much data off the reader
	buff := make([]byte, bsz)
	if n, err = rdr.Read(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to read ingest state")
		return
	}

	// Finally, decode the JSON
	err = json.Unmarshal(buff, s)

	return
}
