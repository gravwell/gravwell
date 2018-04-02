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
	"time"
)

const (
	TS_SIZE int = 12

	secondsPerMinute       = 60
	secondsPerHour         = 60 * 60
	secondsPerDay          = 24 * secondsPerHour
	secondsPerWeek         = 7 * secondsPerDay
	daysPer400Years        = 365*400 + 97
	daysPer100Years        = 365*100 + 24
	daysPer4Years          = 365*4 + 1
	unixToInternal   int64 = (1969*365 + 1969/4 - 1969/100 + 1969/400) * secondsPerDay
)

var (
	ErrTSDataSizeInvalid = errors.New("byte slice size invalid")
)

// Encode writes the timestamp to a buffer, it is designed to be fast and inlined,
// it DOES NOT check the buffer size, so the caller better
func (t Timestamp) Encode(buff []byte) {
	binary.LittleEndian.PutUint64(buff, uint64(t.Sec))
	binary.LittleEndian.PutUint32(buff[8:], uint32(t.Nsec))
}

// Decode reads the timestamp from a buffer, it is designed to be fast and inlined
// it DOES NOT check the buffer size, so the caller better
func (t *Timestamp) Decode(buff []byte) {
	t.Sec = int64(binary.LittleEndian.Uint64(buff))
	t.Nsec = int64(binary.LittleEndian.Uint32(buff[8:]))
}

// Timestamp is the base timestamp structure
// all timestamps are assumed to be UTC
// Sec is the second count since 0000-00-00 00:00:00 UTC
// Nsec is the nanosecond offset from Sec
type Timestamp struct {
	Sec  int64
	Nsec int64
}

// Now retrieves the current UTC time
func Now() Timestamp {
	return FromStandard(time.Now())
}

// FromStandard converts the time.Time datatype to our Timestamp format
func FromStandard(ts time.Time) Timestamp {
	ts = ts.UTC()
	return Timestamp{
		Sec:  ts.Unix() + unixToInternal,
		Nsec: int64(ts.Nanosecond()),
	}
}

func UnixTime(s, ns int64) Timestamp {
	return Timestamp{
		Sec:  s + unixToInternal,
		Nsec: ns,
	}
}

// StandardTime converts our Timestamp format to the golang time.Time datatype
func (t Timestamp) StandardTime() time.Time {
	return time.Unix(t.Sec-unixToInternal, t.Nsec)
}

// Format prints the Timestamp in text using the same layout format as time.Time
func (t Timestamp) Format(layout string) string {
	return t.StandardTime().Format(layout)
}

// String returns the standard string representation of Timestamp
func (t Timestamp) String() string {
	return t.StandardTime().Format(`2006-01-02 15:04:05.999999999 -0700 MST`)
}

// Before returns whether time instance t is before the parameter time tt
func (t Timestamp) Before(tt Timestamp) bool {
	return t.Sec < tt.Sec || t.Sec == tt.Sec && t.Nsec < tt.Nsec
}

// After returns whether time instance t is after parameter time tt
func (t Timestamp) After(tt Timestamp) bool {
	return t.Sec > tt.Sec || t.Sec == tt.Sec && t.Nsec > tt.Nsec
}

// Equal returns whether time instance t is identical to parameter time tt
func (t Timestamp) Equal(tt Timestamp) bool {
	return t.Sec == tt.Sec && t.Nsec == tt.Nsec
}

// MarshalBinary marshals the timestamp into an 12 byte byte slice
func (t Timestamp) MarshalBinary() ([]byte, error) {
	br := make([]byte, 12)
	t.Encode(br)
	return br, nil
}

// MarshalJSON marshals the timestamp into the golang time.Time JSON format
// this is a total hack, and we will write our own marshallers soon
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return t.StandardTime().MarshalJSON()
}

// MarshalText marshals the timestamp into the golang time.Time text format
// this is a total hack, and we will write our own marshallers soon
func (t Timestamp) MarshalText() ([]byte, error) {
	return t.StandardTime().MarshalJSON()
}

// UnmarshalBinary unmarshals a 12 byte encoding of a Timestamp
// This type is NOT compatible with the time.Time format
func (t *Timestamp) UnmarshalBinary(data []byte) error {
	if len(data) < TS_SIZE {
		return ErrTSDataSizeInvalid
	}
	t.Decode(data)
	return nil
}

// UnmarshalJSON unmarshals the timestamp JSON into the Timestamp format
// This format is compatible with the time.Time format
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var ts time.Time
	if err := ts.UnmarshalJSON(data); err != nil {
		return err
	}
	*t = FromStandard(ts)
	return nil
}

// UnmarshalText unmarshals the Timestamp from a byte array
// the format is compatible with the golang time.Time Text format
func (t *Timestamp) UnmarshalText(data []byte) error {
	var ts time.Time
	if err := ts.UnmarshalText(data); err != nil {
		return err
	}
	*t = FromStandard(ts)
	return nil
}

// Since returns the time since the Timestamp tt in as the golang datatype time.Duration
func Since(tt Timestamp) time.Duration {
	return Now().Sub(tt)
}

// Sub subtracts Timestamp tt from Timestamp t and returns the golang time.Duration datatype which is essentially a nanosecond count
func (t Timestamp) Sub(tt Timestamp) time.Duration {
	return time.Duration(t.Sec-tt.Sec)*time.Second + time.Duration(t.Nsec-tt.Nsec)
}

// Add subtracts Timestamp tt from Timestamp t and returns the golang time.Duration datatype which is essentially a nanosecond count
func (t Timestamp) Add(d time.Duration) (tt Timestamp) {
	s := int64(d/1e9) + t.Sec
	n := t.Nsec + int64(d%1e9)
	if n >= 1e9 {
		s++
		n -= 1e9
	} else if n < 0 {
		s--
		n += 1e9
	}
	return Timestamp{
		Sec:  s,
		Nsec: n,
	}
}

// IsZero returns whether the second and nanosecond components are both zero
func (t Timestamp) IsZero() bool {
	return t.Sec == 0 && t.Nsec == 0
}
