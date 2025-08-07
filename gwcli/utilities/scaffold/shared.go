package scaffold

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"golang.org/x/exp/constraints"
)

// This file provides functionality shared across multiple scaffolds.
// Typically, this means functionality for edit and create.

// A KeyedTI is tuple for associating a TI with its field key and whether or not it is required
type KeyedTI struct {
	Key        string          // key to look up the related field in a config map (if applicable)
	FieldTitle string          // text to display to the left of the TI
	TI         textinput.Model // ti for user modifications
	Required   bool            // this TI must have data in it
}

func ViewKTIs(fieldWidth uint, ktis []KeyedTI, selectedIdx uint) string {
	if fieldWidth == 0 {
		clilog.Writer.Warnf("field width is unset")
	}
	//fieldWidth := c.longestFieldLength + 3 // 1 spaces for ":", 1 for pip, 1 for padding

	var ( // styles
		leftAlignerSty = lipgloss.NewStyle().
			Width(int(fieldWidth)).
			AlignHorizontal(lipgloss.Right).
			PaddingRight(1)
	)

	var fields []string
	var TIs []string

	for i, kti := range ktis {
		var sty = stylesheet.Cur.SecondaryText
		if kti.Required {
			sty = stylesheet.Cur.PrimaryText
		}
		title := sty.Render(kti.FieldTitle + ":")

		fields = append(fields, leftAlignerSty.Render(stylesheet.Pip(selectedIdx, uint(i))+title))

		TIs = append(TIs, kti.TI.View())
	}

	// compose all fields
	f := lipgloss.JoinVertical(lipgloss.Right, fields...)

	// compose all TIs
	t := lipgloss.JoinVertical(lipgloss.Left, TIs...)

	// conjoin fields and TIs
	return lipgloss.JoinHorizontal(lipgloss.Center, f, t)
}

// Id_t is the set of constraints defining what can be used as an id for some scaffolds.
// It can be expanded to more types (strings, mayhaps?) if need be, but make sure FromString is expanded, too.
type Id_t interface {
	constraints.Integer | uuid.UUID
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
	default:
		return ret, fmt.Errorf("unknown id type %#v", p)
	}
	return ret, err
}
