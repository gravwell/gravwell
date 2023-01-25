// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package wineventlog

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb

	maxConfigBufferSize = mb
)

// Errors
var (
	// ErrorEvtVarTypeNull is an error that means the content of the EVT_VARIANT
	// data is null.
	ErrorEvtVarTypeNull = errors.New("null EVT_VARIANT data")
)

type InsufficientBufferError struct {
	Cause        error
	RequiredSize int
}

// bookmarkTemplate is a parameterized string that requires two parameters,
// the channel name and the record ID. The formatted string can be used to open
// a new event log subscription and resume from the given record ID.
const bookmarkTemplate = `<BookmarkList><Bookmark Channel="%s" RecordId="%d" ` +
	`IsCurrent="True"/></BookmarkList>`

var providerNameContext EvtHandle

func init() {
	if avail, _ := IsAvailable(); avail {
		providerNameContext, _ = CreateRenderContext([]string{"Event/System/Provider/@Name"}, EvtRenderContextValues)
	}
}

// IsAvailable returns true if the Windows Event Log API is supported by this
// operating system. If not supported then false is returned with the
// accompanying error.
func IsAvailable() (bool, error) {
	err := modwevtapi.Load()
	if err != nil {
		return false, err
	}

	return true, nil
}

// Channels returns a list of channels that are registered on the computer.
func Channels() ([]string, error) {
	handle, err := _EvtOpenChannelEnum(0, 0)
	if err != nil {
		return nil, err
	}
	defer _EvtClose(handle)

	var channels []string
	cpBuffer := make([]uint16, 512)
loop:
	for {
		var used uint32
		err := _EvtNextChannelPath(handle, uint32(len(cpBuffer)), &cpBuffer[0], &used)
		if err != nil {
			errno, ok := err.(syscall.Errno)
			if ok {
				switch errno {
				case ERROR_INSUFFICIENT_BUFFER:
					// Grow buffer.
					newLen := 2 * len(cpBuffer)
					if int(used) > newLen {
						newLen = int(used)
					}
					cpBuffer = make([]uint16, newLen)
					continue
				case ERROR_NO_MORE_ITEMS:
					break loop
				}
			}
			return nil, err
		}
		channels = append(channels, syscall.UTF16ToString(cpBuffer[:used]))
	}

	return channels, nil
}

// EvtOpenLog gets a handle to a channel or log file that you can then use to
// get information about the channel or log file.
func EvtOpenLog(session EvtHandle, path string, flags EvtOpenLogFlag) (EvtHandle, error) {
	var err error
	var pathPtr *uint16
	if path != "" {
		pathPtr, err = syscall.UTF16PtrFromString(path)
		if err != nil {
			return 0, err
		}
	}

	return _EvtOpenLog(session, pathPtr, uint32(flags))
}

// EvtGetLogInfo executes the GetLogInfo syscall to provide information about an open log handle
func EvtGetLogInfo(session EvtHandle, id EvtLogPropertyId) (buff []byte, err error) {
	buff = make([]byte, 64)
	var used uint32
	//first figure out how big of a buffer we need
	if err = _EvtGetLogInfo(session, id, uint32(len(buff)), &buff[0], &used); err == nil {
		//good call
		if used < uint32(len(buff)) {
			buff = buff[:used]
		}
		return
	} else if err != ERROR_INSUFFICIENT_BUFFER {
		buff = nil
		return
	}

	//try again with a bigger buffer
	if used > (16 * mb) {
		buff = nil
		err = fmt.Errorf("GetLogInfo requested too large of a buffer %d", used)
		return
	}
	buff = make([]byte, used)
	used = 0
	if err = _EvtGetLogInfo(session, id, uint32(len(buff)), &buff[0], &used); err != nil {
		buff = nil
	} else if used < uint32(len(buff)) {
		buff = buff[:used]
	}
	return
}

// EvtOpenChannelConfig opens a handle on a channel subscription that represents the channel config
func EvtOpenChannelConfig(path string) (handle EvtHandle, err error) {
	var pathPtr *uint16
	if path == `` {
		err = fmt.Errorf("Channel path is empty")
		return
	}
	if pathPtr, err = syscall.UTF16PtrFromString(path); err != nil {
		return
	}
	handle, err = _EvtOpenChannelConfig(0, pathPtr)
	return
}

// EvtGetChannelConfigProperty queries a channel configuration variable given a handle to the channel configuration
func EvtGetChannelConfigProperty(handle EvtHandle, id EvtChannelConfigPropertyId) (buff []byte, err error) {
	var used uint32
	//figure out how big its supposed to be
	err = _EvtGetChannelConfigProperty(handle, id, 0, nil, &used)
	if err != ERROR_INSUFFICIENT_BUFFER {
		return
	}
	if used > maxConfigBufferSize {
		err = fmt.Errorf("buffer request is too large: %d > %d", used, maxConfigBufferSize)
		return
	}
	buff = make([]byte, used)
	used = 0
	err = _EvtGetChannelConfigProperty(handle, id, uint32(len(buff)), &buff[0], &used)
	return
}

// GetChannelFilePath queries a channel to get the full path of the file that backs it
func GetChannelFilePath(ch string) (pth string, err error) {
	var hnd EvtHandle
	var buff []byte
	if hnd, err = EvtOpenChannelConfig(ch); err != nil {
		return
	}
	if buff, err = EvtGetChannelConfigProperty(hnd, EvtChannelLoggingConfigLogFilePath); err != nil {
		Close(hnd)
		return
	}
	if pth, err = VarantString(buff); err != nil {
		Close(hnd)
		return
	}
	err = Close(hnd)
	return
}

func GetChannelFileCreationTime(ch string) (ts time.Time, err error) {
	var buff []byte
	var ft syscall.Filetime
	var hnd EvtHandle
	if hnd, err = EvtOpenLog(0, ch, 1); err != nil {
		return
	}
	if buff, err = EvtGetLogInfo(hnd, EvtLogCreationTime); err != nil {
		Close(hnd)
		return
	}

	if l := len(buff); l != 16 {
		err = fmt.Errorf("Invalid response buffer size: %d != 16", l)
		Close(hnd)
	} else if v := binary.LittleEndian.Uint32(buff[l-4:]); v != 17 {
		err = fmt.Errorf("Invalid response type: %d != 17", v)
		Close(hnd)
	} else {
		ft.LowDateTime = binary.LittleEndian.Uint32(buff)
		ft.HighDateTime = binary.LittleEndian.Uint32(buff[4:])
		ts = time.Unix(0, ft.Nanoseconds())
		err = Close(hnd)
	}
	return
}

// EvtQuery runs a query to retrieve events from a channel or log file that
// match the specified query criteria.
func EvtQuery(session EvtHandle, path string, query string, flags EvtQueryFlag) (EvtHandle, error) {
	var err error
	var pathPtr *uint16
	if path != "" {
		pathPtr, err = syscall.UTF16PtrFromString(path)
		if err != nil {
			return 0, err
		}
	}

	var queryPtr *uint16
	if query != "" {
		queryPtr, err = syscall.UTF16PtrFromString(query)
		if err != nil {
			return 0, err
		}
	}

	return _EvtQuery(session, pathPtr, queryPtr, uint32(flags))
}

// Subscribe creates a new subscription to an event log channel.
func Subscribe(
	session EvtHandle,
	event windows.Handle,
	channelPath string,
	query string,
	bookmark EvtHandle,
	flags EvtSubscribeFlag,
) (EvtHandle, error) {
	var err error
	var cp *uint16
	if channelPath != "" {
		cp, err = syscall.UTF16PtrFromString(channelPath)
		if err != nil {
			return 0, err
		}
	}

	var q *uint16
	if query != "" {
		q, err = syscall.UTF16PtrFromString(query)
		if err != nil {
			return 0, err
		}
	}

	eventHandle, err := _EvtSubscribe(session, uintptr(event), cp, q, bookmark,
		0, 0, flags)
	if err != nil {
		return 0, err
	}

	return eventHandle, nil
}

// EvtSeek seeks to a specific event in a query result set.
func EvtSeek(resultSet EvtHandle, position int64, bookmark EvtHandle, flags EvtSeekFlag) error {
	_, err := _EvtSeek(resultSet, position, bookmark, 0, uint32(flags))
	return err
}

// EventHandles reads the event handles from a subscription. It attempt to read
// at most maxHandles. ErrorNoMoreHandles is returned when there are no more
// handles available to return. Close must be called on each returned EvtHandle
// when finished with the handle.
func EventHandles(subscription EvtHandle, maxHandles int) ([]EvtHandle, error) {
	if maxHandles < 1 {
		return nil, fmt.Errorf("maxHandles must be greater than 0")
	}

	eventHandles := make([]EvtHandle, maxHandles)
	var numRead uint32

	err := _EvtNext(subscription, uint32(len(eventHandles)),
		&eventHandles[0], 0, 0, &numRead)
	if err != nil {
		// Munge ERROR_INVALID_OPERATION to ERROR_NO_MORE_ITEMS when no handles
		// were read. This happens you call the method and there are no events
		// to read (i.e. polling).
		if err == ERROR_INVALID_OPERATION && numRead == 0 {
			return nil, ERROR_NO_MORE_ITEMS
		}
		return nil, err
	}

	return eventHandles[:numRead], nil
}

// RenderEventSimple reads event data associated with an EvtHandle and renders
// the data using a simple XML.  This function DOES NOT attempt to resolve
// publisher metadata nor does it use the FormatEventString functionality
// GRAVWELL NOTE/TODO - We have yet to see the FormatEventString call succeed
// it always fails in the OpenPublisherMetadata call, which fails with an error about
// not being able to find the specified file.  The call to OpenPublisherMetdata also
// incurs SIGNIFICANT performance overhead, slowing rendering down a 1000 fold and
// taxing the host system.
func RenderEventSimple(eh EvtHandle, buf []byte, out io.Writer) error {
	return RenderEventXML(eh, buf, out)
}

// RenderEventXML renders the event as XML. If the event is already rendered, as
// in a forwarded event whose content type is "RenderedText", then the XML will
// include the RenderingInfo (message). If the event is not rendered then the
// XML will not include the message, and in this case RenderEvent should be
// used.
func RenderEventXML(eventHandle EvtHandle, renderBuf []byte, out io.Writer) error {
	return renderXML(eventHandle, EvtRenderEventXml, renderBuf, out)
}

// RenderBookmarkXML renders a bookmark as XML.
func RenderBookmarkXML(bookmarkHandle EvtHandle, renderBuf []byte, out io.Writer) error {
	return renderXML(bookmarkHandle, EvtRenderBookmark, renderBuf, out)
}

// CreateBookmarkFromRecordID creates a new bookmark pointing to the given recordID
// within the supplied channel. Close must be called on returned EvtHandle when
// finished with the handle.
func CreateBookmarkFromRecordID(channel string, recordID uint64) (EvtHandle, error) {
	xml := fmt.Sprintf(bookmarkTemplate, channel, recordID)
	p, err := syscall.UTF16PtrFromString(xml)
	if err != nil {
		return 0, err
	}

	h, err := _EvtCreateBookmark(p)
	if err != nil {
		return 0, err
	}

	return h, nil
}

// CreateBookmarkFromEvent creates a new bookmark pointing to the given event.
// Close must be called on returned EvtHandle when finished with the handle.
func CreateBookmarkFromEvent(handle EvtHandle) (EvtHandle, error) {
	h, err := _EvtCreateBookmark(nil)
	if err != nil {
		return 0, err
	}
	if err = _EvtUpdateBookmark(h, handle); err != nil {
		return 0, err
	}
	return h, nil
}

// CreateBookmark just creates a new empty bookmark
// caller must close the returned handle
func CreateBookmark() (EvtHandle, error) {
	return _EvtCreateBookmark(nil)
}

// UpdateBookmarkFromEvent Updates an existing bookmark from using an event handle
// This function just wraps the unexported version
func UpdateBookmarkFromEvent(bookmark, handle EvtHandle) error {
	return _EvtUpdateBookmark(bookmark, handle)
}

type Bookmark struct {
	RecordId uint64 `xml:",attr"`
}

type BookmarkList struct {
	Bookmarks []Bookmark `xml:"Bookmark"`
}

// GetBookmarkRecordId takes a bookmark handle, renders it to XML
// we the parse the XML to extract the record id and hand it back
func GetRecordIDFromBookmark(bookmark EvtHandle, buff []byte, bb *bytes.Buffer) (r uint64, err error) {
	if buff == nil {
		buff = make([]byte, 4*1024)
	}
	if bb == nil {
		bb = bytes.NewBuffer(nil)
	}
	if err = RenderBookmarkXML(bookmark, buff, bb); err != nil {
		return
	}
	var v BookmarkList
	if err = xml.Unmarshal(bb.Bytes(), &v); err != nil {
		return
	}
	if len(v.Bookmarks) != 1 {
		err = fmt.Errorf("Invalid rendered bookmarklist, count %d != 1", len(v.Bookmarks))
		return
	}
	r = v.Bookmarks[0].RecordId
	return
}

// CreateBookmarkFromXML creates a new bookmark from the serialised representation
// of an existing bookmark. Close must be called on returned EvtHandle when
// finished with the handle.
func CreateBookmarkFromXML(bookmarkXML string) (EvtHandle, error) {
	xml, err := syscall.UTF16PtrFromString(bookmarkXML)
	if err != nil {
		return 0, err
	}
	return _EvtCreateBookmark(xml)
}

// CreateRenderContext creates a render context. Close must be called on
// returned EvtHandle when finished with the handle.
func CreateRenderContext(valuePaths []string, flag EvtRenderContextFlag) (EvtHandle, error) {
	var paths []uintptr
	for _, path := range valuePaths {
		utf16, err := syscall.UTF16FromString(path)
		if err != nil {
			return 0, err
		}

		paths = append(paths, reflect.ValueOf(&utf16[0]).Pointer())
	}

	var pathsAddr uintptr
	if len(paths) > 0 {
		pathsAddr = reflect.ValueOf(&paths[0]).Pointer()
	}

	context, err := _EvtCreateRenderContext(uint32(len(paths)), pathsAddr, flag)
	if err != nil {
		return 0, err
	}

	return context, nil
}

// OpenPublisherMetadata opens a handle to the publisher's metadata. Close must
// be called on returned EvtHandle when finished with the handle.
func OpenPublisherMetadata(
	session EvtHandle,
	publisherName string,
	lang uint32,
) (EvtHandle, error) {
	p, err := syscall.UTF16PtrFromString(publisherName)
	if err != nil {
		return 0, err
	}

	h, err := _EvtOpenPublisherMetadata(session, p, nil, lang, 0)
	if err != nil {
		return 0, err
	}

	return h, nil
}

// Close closes an EvtHandle.
func Close(h EvtHandle) error {
	return _EvtClose(h)
}

// FormatEventString formats part of the event as a string.
// messageFlag determines what part of the event is formatted as as string.
// eventHandle is the handle to the event.
// publisher is the name of the event's publisher.
// publisherHandle is a handle to the publisher's metadata as provided by
// EvtOpenPublisherMetadata.
// lang is the language ID.
// buffer is optional and if not provided it will be allocated. If the provided
// buffer is not large enough then an InsufficientBufferError will be returned.
func FormatEventString(
	messageFlag EvtFormatMessageFlag,
	eventHandle EvtHandle,
	publisher string,
	publisherHandle EvtHandle,
	lang uint32,
	buffer []byte,
	out io.Writer,
) error {
	// Open a publisher handle if one was not provided.
	ph := publisherHandle
	if ph == 0 {
		ph, err := OpenPublisherMetadata(0, publisher, 0)
		if err != nil {
			return err
		}
		defer _EvtClose(ph)
	}

	// Create a buffer if one was not provided.
	var bufferUsed uint32
	if buffer == nil {
		err := _EvtFormatMessage(ph, eventHandle, 0, 0, 0, messageFlag,
			0, nil, &bufferUsed)
		if err != nil && err != ERROR_INSUFFICIENT_BUFFER {
			return err
		}

		bufferUsed *= 2
		buffer = make([]byte, bufferUsed)
		bufferUsed = 0
	}

	err := _EvtFormatMessage(ph, eventHandle, 0, 0, 0, messageFlag,
		uint32(len(buffer)/2), &buffer[0], &bufferUsed)
	bufferUsed *= 2
	if err == ERROR_INSUFFICIENT_BUFFER {
		return InsufficientBufferError{Cause: err, RequiredSize: int(bufferUsed)}
	}
	if err != nil {
		return err
	}

	// This assumes there is only a single string value to read. This will
	// not work to read keys (when messageFlag == EvtFormatMessageKeyword).
	return UTF16LEBufferToUTF8Writer(buffer[:bufferUsed], out)
}

// offset reads a pointer value from the reader then calculates an offset from
// the start of the buffer to the pointer location. If the pointer value is
// NULL or is outside of the bounds of the buffer then an error is returned.
// reader will be advanced by the size of a uintptr.
func offset(buffer []byte, reader io.Reader) (uint64, error) {
	// Handle 32 and 64-bit pointer size differences.
	var dataPtr uint64
	var err error
	switch runtime.GOARCH {
	default:
		return 0, fmt.Errorf("Unhandled architecture: %s", runtime.GOARCH)
	case "amd64":
		err = binary.Read(reader, binary.LittleEndian, &dataPtr)
		if err != nil {
			return 0, err
		}
	case "386":
		var p uint32
		err = binary.Read(reader, binary.LittleEndian, &p)
		if err != nil {
			return 0, err
		}
		dataPtr = uint64(p)
	}

	if dataPtr == 0 {
		return 0, ErrorEvtVarTypeNull
	}

	bufferPtr := uint64(reflect.ValueOf(&buffer[0]).Pointer())
	offset := dataPtr - bufferPtr

	if offset < 0 || offset > uint64(len(buffer)) {
		return 0, fmt.Errorf("Invalid pointer %x. Cannot dereference an "+
			"address outside of the buffer [%x:%x].", dataPtr, bufferPtr,
			bufferPtr+uint64(len(buffer)))
	}

	return offset, nil
}

// readString reads a pointer using the reader then parses the UTF-16 string
// that the pointer addresses within the buffer.
func readString(buffer []byte, reader io.Reader) (string, error) {
	offset, err := offset(buffer, reader)
	if err != nil {
		// Ignore NULL values.
		if err == ErrorEvtVarTypeNull {
			return "", nil
		}
		return "", err
	}

	return UTF16LEToUTF8(buffer[offset:])
}

// evtRenderProviderName renders the ProviderName of an event.
func evtRenderProviderName(renderBuf []byte, eventHandle EvtHandle) (string, error) {
	var bufferUsed, propertyCount uint32
	err := _EvtRender(providerNameContext, eventHandle, EvtRenderEventValues,
		uint32(len(renderBuf)), &renderBuf[0], &bufferUsed, &propertyCount)
	if err == ERROR_INSUFFICIENT_BUFFER {
		return "", InsufficientBufferError{Cause: err, RequiredSize: int(bufferUsed)}
	}
	if err != nil {
		return "", fmt.Errorf("evtRenderProviderName %v", err)
	}

	reader := bytes.NewReader(renderBuf)
	return readString(renderBuf, reader)
}

func renderXML(eventHandle EvtHandle, flag EvtRenderFlag, renderBuf []byte, out io.Writer) error {
	var bufferUsed, propertyCount uint32

	err := _EvtRender(0, eventHandle, flag, uint32(len(renderBuf)),
		&renderBuf[0], &bufferUsed, &propertyCount)
	if err == ERROR_INSUFFICIENT_BUFFER {
		return InsufficientBufferError{Cause: err, RequiredSize: int(bufferUsed)}
	}
	if err != nil {
		return err
	}

	if int(bufferUsed) > len(renderBuf) {
		return fmt.Errorf("Windows EvtRender reported that wrote %d bytes "+
			"to the buffer, but the buffer can only hold %d bytes",
			bufferUsed, len(renderBuf))
	}
	return UTF16LEBufferToUTF8Writer(renderBuf[:bufferUsed], out)
}

func VarantString(buff []byte) (s string, err error) {
	var v uint32
	if len(buff) < 16 || (len(buff)%2) != 0 {
		err = fmt.Errorf("invalid varant buffer")
		return
	}
	//varant strings always have a count of zero
	if v = binary.LittleEndian.Uint32(buff[8:]); v != 0 {
		err = fmt.Errorf("Invalid variant count: %d != 0", v)
		return
	} else if v = binary.LittleEndian.Uint32(buff[12:]); v != 1 {
		//make sure the type is a string
		err = fmt.Errorf("Invalid variant type: %d != 1", v)
		return
	}
	s, _, err = UTF16BytesToString(buff[16:])
	return
}

// UTF16BytesToString returns a string that is decoded from the UTF-16 bytes.
// The byte slice must be of even length otherwise an error will be returned.
// The integer returned is the offset to the start of the next string with
// buffer if it exists, otherwise -1 is returned.
func UTF16BytesToString(b []byte) (string, int, error) {
	if len(b)%2 != 0 {
		return "", 0, fmt.Errorf("Slice must have an even length (length=%d)", len(b))
	}

	offset := -1

	// Find the null terminator if it exists and re-slice the b.
	if nullIndex := indexNullTerminator(b); nullIndex > -1 {
		if len(b) > nullIndex+2 {
			offset = nullIndex + 2
		}

		b = b[:nullIndex]
	}

	s := make([]uint16, len(b)/2)
	for i := range s {
		s[i] = uint16(b[i*2]) + uint16(b[(i*2)+1])<<8
	}

	return string(utf16.Decode(s)), offset, nil
}

// indexNullTerminator returns the index of a null terminator within a buffer
// containing UTF-16 encoded data. If the null terminator is not found -1 is
// returned.
func indexNullTerminator(b []byte) int {
	if len(b) < 2 {
		return -1
	}

	for i := 0; i < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			return i
		}
	}

	return -1
}

type LogFileInfo struct {
	Attributes      uint32
	LastWrite       time.Time
	Creation        time.Time
	NumberOfRecords uint64
	OldestRecord    uint64
}

func QueryLogFile(hnd EvtHandle) (lfi LogFileInfo, err error) {
	var buff []byte
	if buff, err = EvtGetLogInfo(hnd, EvtLogAttributes); err != nil {
		return
	} else if l := len(buff); l >= 16 && binary.LittleEndian.Uint32(buff[l-4:]) == 8 {
		lfi.Attributes = binary.LittleEndian.Uint32(buff)
	}

	if buff, err = EvtGetLogInfo(hnd, EvtLogLastWriteTime); err != nil {
		return
	} else if l := len(buff); l >= 16 && binary.LittleEndian.Uint32(buff[l-4:]) == 17 {
		var ft syscall.Filetime
		ft.LowDateTime = binary.LittleEndian.Uint32(buff)
		ft.HighDateTime = binary.LittleEndian.Uint32(buff[4:])
		lfi.LastWrite = time.Unix(0, ft.Nanoseconds()).UTC()
	}
	if buff, err = EvtGetLogInfo(hnd, EvtLogCreationTime); err != nil {
		return
	} else if l := len(buff); l >= 16 && binary.LittleEndian.Uint32(buff[l-4:]) == 17 {
		var ft syscall.Filetime
		ft.LowDateTime = binary.LittleEndian.Uint32(buff)
		ft.HighDateTime = binary.LittleEndian.Uint32(buff[4:])
		lfi.Creation = time.Unix(0, ft.Nanoseconds()).UTC()
	}

	if buff, err = EvtGetLogInfo(hnd, EvtLogNumberOfLogRecords); err != nil {
		return
	} else if l := len(buff); l >= 16 && binary.LittleEndian.Uint32(buff[l-4:]) == 10 {
		lfi.NumberOfRecords = binary.LittleEndian.Uint64(buff)
	}

	if buff, err = EvtGetLogInfo(hnd, EvtLogOldestRecordNumber); err != nil {
		return
	} else if l := len(buff); l >= 16 && binary.LittleEndian.Uint32(buff[l-4:]) == 10 {
		lfi.OldestRecord = binary.LittleEndian.Uint64(buff)
	}

	return
}

func (ibe InsufficientBufferError) Error() string {
	if ibe.Cause == nil {
		return ``
	}
	return fmt.Sprintf("%v need %d", ibe.Cause, ibe.RequiredSize)
}
