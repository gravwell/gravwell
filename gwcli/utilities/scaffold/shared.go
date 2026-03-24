package scaffold

import (
	"fmt"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"golang.org/x/exp/constraints"
)

// This file provides functionality shared across multiple scaffolds.
// Typically, this means functionality for edit and create.

// A KeyedFP associates metadata to a filepicker.
type KeyedFP struct {
	Key        string // key to look up the related field in a config map (if applicable)
	FieldTitle string // text to display to the left of the path
	FP         filepicker.Model
	Required   bool
}

// Id_t is the set of constraints defining what can be used as an id for some scaffolds.
// It can be expanded to more types (strings, mayhaps?) if need be, but make sure FromString is expanded, too.
type Id_t interface {
	constraints.Integer | uuid.UUID | string
}

// FromString returns str converted to an id of type I.
// All hail the modern Library of Alexandria (https://stackoverflow.com/a/71048872).
func FromString[I Id_t](str string) (I, error) {
	var (
		err error
		ret I
	)

	switch p := any(&ret).(type) {
	case *uuid.UUID:
		var u uuid.UUID
		u, err = uuid.Parse(str)
		*p = u
	case *uint:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 64)
		*p = uint(i)
	case *uint8:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 8)
		*p = uint8(i)
	case *uint16:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 16)
		*p = uint16(i)
	case *uint32:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 32)
		*p = uint32(i)
	case *uint64:
		var i uint64
		i, err = strconv.ParseUint(str, 10, 64)
		*p = uint64(i)
	case *int:
		*p, err = strconv.Atoi(str)
	case *int8:
		var i int64
		i, err = strconv.ParseInt(str, 10, 8)
		*p = int8(i)
	case *int16:
		var i int64
		i, err = strconv.ParseInt(str, 10, 16)
		*p = int16(i)
	case *int32:
		var i int64
		i, err = strconv.ParseInt(str, 10, 32)
		*p = int32(i)
	case *int64:
		var i int64
		i, err = strconv.ParseInt(str, 10, 64)
		*p = int64(i)
	case *string:
		*p = str
	default:
		return ret, fmt.Errorf("unknown id type %#v", p)
	}
	return ret, err
}

// IdentifyCaller returns a valid SDParam containing information about the caller to make it easier to log which action is in error.
func IdentifyCaller() rfc5424.SDParam {
	var identifier = rfc5424.SDParam{Name: "caller", Value: "UNKNOWN"}

	// extract the last two elements in the caller's path, skipping all scaffoldlist callers
	var callers = make([]uintptr, 6)
	count := runtime.Callers(3, callers) // skip runtime.Callers, skip ourselves, skip the first scaffold call
	if count == 0 {
		identifier.Value = "<no_callers_returned>"
		return identifier
	}
	frames := runtime.CallersFrames(callers[:count])
	for {
		frame, more := frames.Next()
		// skip scaffoldcreate frames
		if strings.Contains(frame.File, "scaffold") {
			if !more {
				break
			}
			continue
		}
		// trim the paths to just the function and line
		identifier.Value = fmt.Sprintf("%v:%v", path.Base(frame.Function), frame.Line)
		break
	}

	return identifier
}
