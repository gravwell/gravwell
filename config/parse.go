/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb
)

func AppendDefaultPort(bstr string, defPort uint16) string {
	if _, _, err := net.SplitHostPort(bstr); err != nil {
		if strings.HasSuffix(err.Error(), `missing port in address`) {
			return fmt.Sprintf("%s:%d", bstr, defPort)
		}
	}
	return bstr
}

type multSuff struct {
	mult   int64
	suffix string
}

var (
	rateSuffix = []multSuff{
		multSuff{mult: 1024, suffix: `k`},
		multSuff{mult: 1024, suffix: `kb`},
		multSuff{mult: 1024, suffix: `kbit`},
		multSuff{mult: 1024, suffix: `kbps`},
		multSuff{mult: 1024 * 1024, suffix: `m`},
		multSuff{mult: 1024 * 1024, suffix: `mb`},
		multSuff{mult: 1024 * 1024, suffix: `mbit`},
		multSuff{mult: 1024 * 1024, suffix: `mbps`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `g`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `gb`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `gbit`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `gbps`},
	}
)

//we return the rate in bytes per second
func ParseRate(s string) (Bps int64, err error) {
	var r uint64
	if len(s) == 0 {
		return
	}
	s = strings.ToLower(s)
	for _, v := range rateSuffix {
		if strings.HasSuffix(s, v.suffix) {
			s = strings.TrimSuffix(s, v.suffix)
			if r, err = strconv.ParseUint(s, 10, 64); err != nil {
				return
			}
			Bps = (int64(r) * v.mult) / 8
			return
		}
	}
	if r, err = strconv.ParseUint(s, 10, 64); err != nil {
		return
	}
	Bps = int64(r / 8)
	if Bps < minThrottle {
		err = errors.New("Ingest cannot be limited below 1mbit")
	}
	return
}

// ParseSource returns a net.IP byte buffer
// the returned buffer will always be a 32bit or 128bit buffer
// but we accept encodings as IPv4, IPv6, integer, hex encoded hash
// this function simply walks the available encodings until one works
func ParseSource(v string) (b net.IP, err error) {
	var i uint64
	// try as an IP
	if b = net.ParseIP(v); b != nil {
		return
	}
	//try as a plain integer
	if i, err = ParseUint64(v); err == nil {
		//encode into a buffer
		bb := make([]byte, 16)
		binary.BigEndian.PutUint64(bb[8:], i)
		b = net.IP(bb)
		return
	}
	//try as a hex encoded byte array
	if (len(v)&1) == 0 && len(v) <= 32 {
		var vv []byte
		if vv, err = hex.DecodeString(v); err == nil {
			bb := make([]byte, 16)
			offset := len(bb) - len(vv)
			copy(bb[offset:], vv)
			b = net.IP(bb)
			return
		}
	}
	err = fmt.Errorf("Failed to decode %s as a source value", v)
	return
}

func ParseUint64(v string) (i uint64, err error) {
	if strings.HasPrefix(v, "0x") {
		i, err = strconv.ParseUint(strings.TrimPrefix(v, "0x"), 16, 64)
	} else {
		i, err = strconv.ParseUint(v, 10, 64)
	}
	return
}

func ParseInt64(v string) (i int64, err error) {
	if strings.HasPrefix(v, "0x") {
		i, err = strconv.ParseInt(strings.TrimPrefix(v, "0x"), 16, 64)
	} else {
		i, err = strconv.ParseInt(v, 10, 64)
	}
	return
}

// lineParameter checks if the line contains the parameter provided
// the parameter is considered provided if after a ToLower and TrimSpace the parameter is the prefix
// empty lines and/or empty parameters are not checked
// the match is case insensitive
func lineParameter(line, parameter string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	p := strings.ToLower(strings.TrimSpace(parameter))
	if len(l) == 0 || len(p) == 0 {
		return false
	}
	return strings.HasPrefix(l, p)
}

// globalLineBoundary returns the line numbers representing the start and stop boundaries of the global section
// if the global section cannot be found, both returned values are -1
// start is inclusive, stop is exclusive, so normal ranging is appropriate with the bound values
func globalLineBoundary(lines []string) (start, stop int, ok bool) {
	start = -1
	stop = -1
	//find the start of the global section
	for i := range lines {
		if lineParameter(lines[i], globalHeader) {
			start = i
			break
		}
	}
	if start == -1 {
		//did not find the start
		return
	}

	//try to find the end
	for i := start + 1; i < len(lines); i++ {
		if lineParameter(lines[i], headerStart) {
			stop = i
			ok = true
			return
		}
	}
	//not stop found, set to the end
	stop = len(lines)
	if start < 0 || stop < 0 || start > len(lines) || stop > len(lines) || start >= stop {
		//nothing here is valid
		return
	}
	ok = true
	return
}

// argInGlobalLines identifies which line in the global config contains the given parameter argument
// if the argument is not found, -1 is returned
func argInGlobalLines(lines []string, arg string) (lineno int) {
	lineno = -1
	gstart, gend, ok := globalLineBoundary(lines)
	if !ok {
		return
	}
	for i := gstart; i < gend; i++ {
		if lineParameter(lines[i], arg) {
			lineno = i
			return
		}
	}
	return
}

func insertLine(lines []string, line string, loc int) (nl []string, err error) {
	if loc < 0 || loc >= len(lines) {
		err = ErrInvalidLineLocation
		return
	}
	nl = append(nl, lines[0:loc]...)
	nl = append(nl, line)
	nl = append(nl, lines[loc:]...)

	return
}

func getLeadingString(l, param string) (s string) {
	if idx := strings.Index(strings.ToLower(l), strings.ToLower(param)); idx != -1 {
		s = l[0:idx]
	}
	return
}

func getCommentString(l, param string) (s string) {
	if idx := strings.Index(strings.ToLower(l), strings.ToLower(param)); idx != -1 {
		s = l[idx:]
	}
	return
}

// updateLine updates the parameter value at a given line
// the given line MUST contain the paramter value, or we error out
func updateLine(lines []string, param, value string, loc int) (nl []string, err error) {
	//check that the line location is valid
	if loc >= len(lines) || loc < 0 {
		err = ErrInvalidLineLocation
	}
	//check if the specified line has that parameter
	if !lineParameter(lines[loc], param) {
		err = ErrInvalidUpdateLineParameter
		return
	}
	//get the leading stuff
	leadingString := getLeadingString(lines[loc], param)
	//get any trailing comments
	commentString := getCommentString(lines[loc], commentValue)
	nl = lines
	nl[loc] = fmt.Sprintf(`%s%s=%s %s`, leadingString, param, value, commentString)
	return
}
