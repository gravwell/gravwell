/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"time"
)

const (
	//we don't use iota to ensure that any changes screw up cohesion
	typeByteSlice uint8 = 1
	typeBool      uint8 = 2
	typeByte      uint8 = 3
	typeInt8      uint8 = 4
	typeInt16     uint8 = 5
	typeUint16    uint8 = 6
	typeInt32     uint8 = 7
	typeUint32    uint8 = 8
	typeInt64     uint8 = 9
	typeUint64    uint8 = 10
	typeFloat32   uint8 = 11
	typeFloat64   uint8 = 12
	typeUnicode   uint8 = 13 //basically a string
	typeMAC       uint8 = 14 // net.HardwareAddr but basically just a byte slice
	typeIP        uint8 = 15 // Proper net.IP
	typeTS        uint8 = 16 // Time
	typeDuration  uint8 = 17 // in and out as a time.Duration, but is an int64 internally
)

var (
	ErrUnknownType = errors.New("unknown native type")
)

type EnumeratedData struct {
	data   []byte
	evtype uint8 //you don't get access to this, sorry
}

// InverEnumeratedData takes a native type and creates a properly annotated enumerated value data section.
// The function returns an empty EnumeratedData if the type provided type is invalid.
func InferEnumeratedData(val interface{}) (EnumeratedData, error) {
	switch v := val.(type) {
	case bool:
		return BoolEnumData(v), nil
	case uint8:
		return ByteEnumData(v), nil
	case int8:
		return Int8EnumData(v), nil
	case int16:
		return Int16EnumData(v), nil
	case uint16:
		return Uint16EnumData(v), nil
	case int32:
		return Int32EnumData(v), nil
	case uint32:
		return Uint32EnumData(v), nil
	case int:
		return Int64EnumData(int64(v)), nil
	case int64:
		return Int64EnumData(v), nil
	case uint:
		return Uint64EnumData(uint64(v)), nil
	case uint64:
		return Uint64EnumData(v), nil
	case float32:
		return Float32EnumData(v), nil
	case float64:
		return Float64EnumData(v), nil
	case string:
		if len(v) > MaxEvDataLength {
			return EnumeratedData{}, fmt.Errorf("Enumerated Data string is too large")
		}
		return StringEnumData(v), nil
	case []byte:
		if len(v) > MaxEvDataLength {
			return EnumeratedData{}, fmt.Errorf("Enumerated Data byteslice is too large")
		}
		return SliceEnumData(v), nil
	case net.IP:
		if l := len(v); l != 4 && l != 16 {
			return EnumeratedData{}, fmt.Errorf("invalid IP length")
		}
		return IPEnumData(v), nil
	case net.HardwareAddr:
		if l := len(v); l == 0 || l > 20 || (l%2) != 0 {
			return EnumeratedData{}, fmt.Errorf("invalid HardwareAddr length")
		}
		return MACEnumData(v), nil
	case time.Time:
		return TSEnumData(FromStandard(v)), nil
	case Timestamp:
		return TSEnumData(v), nil
	case time.Duration:
		return DurationEnumData(v), nil
	}

	//unknown type
	return EnumeratedData{}, ErrUnknownType
}

// BoolEnumData creates an EnumeratedData from a native boolean.
func BoolEnumData(v bool) EnumeratedData {
	val := []byte{0}
	if v {
		val[0] = 1
	}
	return EnumeratedData{
		data:   val,
		evtype: typeBool,
	}
}

func ByteEnumData(v byte) EnumeratedData {
	return EnumeratedData{
		data:   []byte{v},
		evtype: typeByte,
	}
}

func Int8EnumData(v int8) EnumeratedData {
	return EnumeratedData{
		data:   []byte{byte(v)},
		evtype: typeInt8,
	}
}

func Int16EnumData(v int16) EnumeratedData {
	dt := make([]byte, 2)
	binary.LittleEndian.PutUint16(dt, uint16(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeInt16,
	}
}

func Int32EnumData(v int32) EnumeratedData {
	dt := make([]byte, 4)
	binary.LittleEndian.PutUint32(dt, uint32(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeInt32,
	}
}

func Int64EnumData(v int64) EnumeratedData {
	dt := make([]byte, 8)
	binary.LittleEndian.PutUint64(dt, uint64(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeInt64,
	}
}

func Uint16EnumData(v uint16) EnumeratedData {
	dt := make([]byte, 2)
	binary.LittleEndian.PutUint16(dt, v)
	return EnumeratedData{
		data:   dt,
		evtype: typeUint16,
	}
}

func Uint32EnumData(v uint32) EnumeratedData {
	dt := make([]byte, 4)
	binary.LittleEndian.PutUint32(dt, v)
	return EnumeratedData{
		data:   dt,
		evtype: typeUint32,
	}
}

func Uint64EnumData(v uint64) EnumeratedData {
	dt := make([]byte, 8)
	binary.LittleEndian.PutUint64(dt, v)
	return EnumeratedData{
		data:   dt,
		evtype: typeUint64,
	}
}

func IntEnumData(v int) EnumeratedData {
	return Int64EnumData(int64(v))
}

func UintEnumData(v uint) EnumeratedData {
	return Uint64EnumData(uint64(v))
}

func Float32EnumData(v float32) EnumeratedData {
	dt := make([]byte, 4)
	binary.LittleEndian.PutUint32(dt, math.Float32bits(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeFloat32,
	}
}

func Float64EnumData(v float64) EnumeratedData {
	dt := make([]byte, 8)
	binary.LittleEndian.PutUint64(dt, math.Float64bits(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeFloat64,
	}
}

func StringEnumData(v string) EnumeratedData {
	return EnumeratedData{
		data:   []byte(v),
		evtype: typeUnicode,
	}
}

func SliceEnumData(v []byte) EnumeratedData {
	return EnumeratedData{
		data:   v,
		evtype: typeByteSlice,
	}
}

func MACEnumData(addr net.HardwareAddr) EnumeratedData {
	return EnumeratedData{
		data:   []byte(addr),
		evtype: typeMAC,
	}
}

func IPEnumData(addr net.IP) EnumeratedData {
	return EnumeratedData{
		data:   []byte(addr),
		evtype: typeIP,
	}
}

func TSEnumData(v Timestamp) EnumeratedData {
	dt := make([]byte, 12)
	v.Encode(dt)
	return EnumeratedData{
		data:   dt,
		evtype: typeTS,
	}
}

func DurationEnumData(v time.Duration) EnumeratedData {
	dt := make([]byte, 8)
	binary.LittleEndian.PutUint64(dt, uint64(v))
	return EnumeratedData{
		data:   dt,
		evtype: typeDuration,
	}
}

// Interface is a helper function that will return an interface populated with the native type.
func (ev EnumeratedData) Interface() (v interface{}) {
	switch ev.evtype {
	case typeBool:
		if len(ev.data) != 1 {
			v = false
		} else {
			v = ev.data[0] != 0
		}
	case typeByte:
		if len(ev.data) != 1 {
			v = byte(0)
		} else {
			v = ev.data[0]
		}
	case typeInt8:
		if len(ev.data) != 1 {
			v = int8(0)
		} else {
			v = int8(ev.data[0])
		}
	case typeInt16:
		if len(ev.data) != 2 {
			v = int16(0)
		} else {
			v = int16(binary.LittleEndian.Uint16(ev.data))
		}
	case typeUint16:
		if len(ev.data) != 2 {
			v = uint16(0)
		} else {
			v = binary.LittleEndian.Uint16(ev.data)
		}
	case typeInt32:
		if len(ev.data) != 4 {
			v = int32(0)
		} else {
			v = int32(binary.LittleEndian.Uint32(ev.data))
		}
	case typeUint32:
		if len(ev.data) != 4 {
			v = uint32(0)
		} else {
			v = uint32(binary.LittleEndian.Uint32(ev.data))
		}
	case typeInt64:
		if len(ev.data) != 8 {
			v = int64(0)
		} else {
			v = int64(binary.LittleEndian.Uint64(ev.data))
		}
	case typeUint64:
		if len(ev.data) != 8 {
			v = uint64(0)
		} else {
			v = binary.LittleEndian.Uint64(ev.data)
		}
	case typeFloat32:
		if len(ev.data) != 4 {
			v = float32(0)
		} else {
			v = math.Float32frombits(binary.LittleEndian.Uint32(ev.data))
		}
	case typeFloat64:
		if len(ev.data) != 8 {
			v = float64(0)
		} else {
			v = math.Float64frombits(binary.LittleEndian.Uint64(ev.data))
		}
	case typeDuration:
		if len(ev.data) != 8 {
			v = time.Duration(0)
		} else {
			v = time.Duration(binary.LittleEndian.Uint64(ev.data))
		}
	//other types
	case typeByteSlice:
		v = ev.data
	case typeUnicode:
		//slice is ambiguous and will break a bunch of usage in anko, so this one doesn't make it out native
		if len(ev.data) == 0 {
			v = ""
		} else {
			v = string(ev.data)
		}
	case typeMAC:
		v = net.HardwareAddr(ev.data)
	case typeIP:
		v = net.IP(ev.data)
	case typeTS:
		var ts Timestamp
		ts.UnmarshalBinary(ev.data)
		v = ts
	}
	return
}

func (ev EnumeratedData) String() string {
	switch ev.evtype {
	case typeBool:
		if len(ev.data) == 1 && ev.data[0] != 0 {
			return `true`
		}
		return `false`
	case typeByte:
		if len(ev.data) == 1 {
			return strconv.FormatUint(uint64(ev.data[0]), 10)
		}
		return ``
	case typeInt8:
		if len(ev.data) == 1 {
			return strconv.FormatInt(int64(ev.data[0]), 10)
		}
	case typeInt16:
		if len(ev.data) == 2 {
			return strconv.FormatInt(int64(binary.LittleEndian.Uint16(ev.data)), 10)
		}
	case typeUint16:
		if len(ev.data) == 2 {
			return strconv.FormatUint(uint64(binary.LittleEndian.Uint16(ev.data)), 10)
		}
	case typeInt32:
		if len(ev.data) == 4 {
			return strconv.FormatInt(int64(binary.LittleEndian.Uint32(ev.data)), 10)
		}
	case typeUint32:
		if len(ev.data) == 4 {
			return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(ev.data)), 10)
		}
	case typeInt64:
		if len(ev.data) == 8 {
			return strconv.FormatInt(int64(binary.LittleEndian.Uint64(ev.data)), 10)
		}
	case typeUint64:
		if len(ev.data) == 8 {
			return strconv.FormatUint(binary.LittleEndian.Uint64(ev.data), 10)
		}
		return ``
	case typeFloat32:
		if len(ev.data) == 4 {
			return strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(ev.data))), 'G', 12, 64)
		}
		return ``
	case typeFloat64:
		if len(ev.data) == 8 {
			return strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(ev.data)), 'G', 12, 64)
		}
		return ``
	case typeUnicode:
		return string(ev.data)
	case typeByteSlice:
		return string(ev.data)
	case typeMAC:
		return net.HardwareAddr(ev.data).String()
	case typeIP:
		return net.IP(ev.data).String()
	case typeTS:
		var ts Timestamp
		ts.UnmarshalBinary(ev.data)
		return ts.String()
	case typeDuration:
		var d time.Duration
		if len(ev.data) == 8 {
			d = time.Duration(binary.LittleEndian.Uint64(ev.data))
		}
		return d.String()
	}
	return `` //return empty string on default
}

// Valid is a helper function that declares if an enumerated data item is valid
// this means that the type is known and the encoded bytes match what is expected.
func (ev EnumeratedData) Valid() bool {
	switch ev.evtype {
	case typeBool:
		fallthrough
	case typeByte:
		fallthrough
	case typeInt8:
		if len(ev.data) == 1 {
			return true
		}
		return false
	case typeInt16:
		fallthrough
	case typeUint16:
		if len(ev.data) == 2 {
			return true
		}
		return false
	case typeInt32:
		fallthrough
	case typeUint32:
		fallthrough
	case typeFloat32:
		if len(ev.data) == 4 {
			return true
		}
		return false
	case typeInt64:
		fallthrough
	case typeUint64:
		fallthrough
	case typeFloat64:
		fallthrough
	case typeDuration:
		if len(ev.data) == 8 {
			return true
		}
		return false
	case typeUnicode:
		fallthrough
	case typeByteSlice:
		if len(ev.data) <= MaxEvDataLength {
			return true // byte slices are always valid, even empty ones
		}
		return false // too large
	case typeMAC:
		if l := len(ev.data); (l%2) == 0 && l <= 20 {
			return true
		}
		return false
	case typeIP:
		if len(ev.data) == 4 || len(ev.data) == 16 {
			return true
		}
		return false
	case typeTS:
		if len(ev.data) == 12 {
			return true
		}
		return false
	}
	return false //bad type
}
