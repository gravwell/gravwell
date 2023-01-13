/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"encoding/binary"
	"encoding/hex"
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

// AppendDefaultPort will append the network port in defPort to the address
// in bstr, provided the address does not already contain a port.
// Thus, AppendDefaultPort("10.0.0.1", 4023) will return "10.0.0.1:4023",
// but AppendDefaultPort("10.0.0.1:5555", 4023) will return "10.0.0.1:5555".
func AppendDefaultPort(bstr string, defPort uint16) string {
	// first, try to parse as a plain IP
	if ip := net.ParseIP(bstr); ip != nil {
		return net.JoinHostPort(bstr, strconv.FormatUint(uint64(defPort), 10))
	}
	if _, _, err := net.SplitHostPort(bstr); err != nil {
		if aerr, ok := err.(*net.AddrError); ok && aerr.Err == "missing port in address" {
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
		multSuff{mult: 1024, suffix: `kbit`},
		multSuff{mult: 1024, suffix: `kbps`},
		multSuff{mult: 1024, suffix: `Kbit`},
		multSuff{mult: 1024, suffix: `Kbps`},
		multSuff{mult: 8 * 1024, suffix: `KBps`},

		multSuff{mult: 1024 * 1024, suffix: `mbit`},
		multSuff{mult: 1024 * 1024, suffix: `mbps`},
		multSuff{mult: 1024 * 1024, suffix: `Mbit`},
		multSuff{mult: 1024 * 1024, suffix: `Mbps`},
		multSuff{mult: 8 * 1024 * 1024, suffix: `MBps`},

		multSuff{mult: 1024 * 1024 * 1024, suffix: `gbit`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `gbps`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `Gbit`},
		multSuff{mult: 1024 * 1024 * 1024, suffix: `Gbps`},
		multSuff{mult: 8 * 1024 * 1024 * 1024, suffix: `GBps`},
	}
)

// ParseRate parses a data rate, returning an integer bits per second.
// The rate string s should consist of numbers optionally followed by one
// of the following suffixes: k, kb, kbit, kbps, m, mb, mbit, mbps, g, gb,
// gbit, gbps.
// If no suffix is present, ParseRate assumes the string specifies
// bits per second.
func ParseRate(s string) (bps int64, err error) {
	var r uint64
	if len(s) == 0 {
		return
	}
	for _, v := range rateSuffix {
		if strings.HasSuffix(s, v.suffix) {
			s = strings.TrimSuffix(s, v.suffix)
			if r, err = strconv.ParseUint(s, 10, 64); err != nil {
				return
			}
			bps = int64(r) * v.mult
			return
		}
	}
	if r, err = strconv.ParseUint(s, 10, 64); err != nil {
		return
	}
	bps = int64(r)
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

// ParseBool attempts to parse the string v into a boolean. The following will
// return true:
//
//   - "true"
//   - "t"
//   - "yes"
//   - "y"
//   - "1"
//
// The following will return false:
//
//   - "false"
//   - "f"
//   - "no"
//   - "n"
//   - "0"
//
// All other values return an error.
func ParseBool(v string) (r bool, err error) {
	v = strings.ToLower(v)
	switch v {
	case `true`:
		fallthrough
	case `t`:
		fallthrough
	case `yes`:
		fallthrough
	case `y`:
		fallthrough
	case `1`:
		r = true
	case `false`:
	case `f`:
	case `0`:
	case `no`:
	case `n`:
	default:
		err = fmt.Errorf("Unknown boolean value")
	}
	return
}

// ParseUint64 will attempt to turn the given string into an unsigned 64-bit integer.
func ParseUint64(v string) (i uint64, err error) {
	if strings.HasPrefix(v, "0x") {
		i, err = strconv.ParseUint(strings.TrimPrefix(v, "0x"), 16, 64)
	} else {
		i, err = strconv.ParseUint(v, 10, 64)
	}
	return
}

// ParseInt64 will attempt to turn the given string into a signed 64-bit integer.
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
