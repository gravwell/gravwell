// Package pflagtypes implements new flag types for use with pflag.Var.
package pflagtypes

import (
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/pflag"
)

// UUIDValue implements google/uuid as a pflag.Value.
type UUIDValue uuid.UUID

// NewUUIDValue clones the given UUID and sets it as the starter value for the returned UUIDValue.
func NewUUIDValue(val uuid.UUID) *UUIDValue {
	my := val
	return (*UUIDValue)(&my)
}

func (v *UUIDValue) String() string {
	return uuid.UUID(*v).String()
}

// Set updates the underlying value of the uuid.
// If the parse fails, UUID is set back to the zero value.
func (v *UUIDValue) Set(s string) error {
	uuid, err := uuid.Parse(s)
	*v = UUIDValue(uuid)
	return err
}

func (v *UUIDValue) Type() string {
	return "uuid"
}

var _ pflag.Value = &UUIDValue{}

// UUIDSliceValue implements a slice of google/uuid as a pflag.Value.
type UUIDSliceValue struct {
	value   *[]uuid.UUID
	changed bool
	sep     rune
}

// NewUUIDSliceValue deep-copies the given UUID slice and sets it as the starter value for the returned UUIDSliceValue.
func NewUUIDSliceValue(val []uuid.UUID, separator rune) *UUIDSliceValue {
	usv := &UUIDSliceValue{}
	my := (slices.Clone(val))
	usv.value = &my
	usv.sep = separator
	return usv
}

func (v *UUIDSliceValue) String() string {
	if v.value == nil || len(*v.value) < 1 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for _, uuid := range *v.value {
		sb.WriteString(uuid.String())
		sb.WriteRune(v.sep)
	}
	return sb.String()[:sb.Len()-1] + "]"
}

// Set splits on ',' and attempts to set the resulting slice.
// It halts on the first error.
// Whitespace values are skipped over. If only whitespace/the empty string are given, the underlying slice will be emptied.
// Only a failed parse returns an error.
func (v *UUIDSliceValue) Set(s string) error {
	exploded := strings.Split(s, string(v.sep))
	new := make([]uuid.UUID, 0, len(exploded))
	for _, val := range exploded {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		uuid, err := uuid.Parse(val)
		if err != nil {
			return err
		}
		new = append(new, uuid)
	}
	new = slices.Clip(new)
	v.value = &new
	return nil
}

func (v *UUIDSliceValue) Type() string {
	return "uuidSlice"
}

var _ pflag.Value = &UUIDValue{}
