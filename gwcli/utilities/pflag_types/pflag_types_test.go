package pflagtypes_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	pflagtypes "github.com/gravwell/gravwell/v4/gwcli/utilities/pflag_types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDValue(t *testing.T) {
	data, err := uuid.NewRandom()
	require.Nil(t, err)
	val := pflagtypes.NewUUIDValue(data)
	assert.Equal(t, "uuid", val.Type())
	assert.Equal(t, data.String(), val.String())

	data, err = uuid.NewRandom()
	require.Nil(t, err)
	assert.Nil(t, val.Set(data.String()))
	assert.Equal(t, data.String(), val.String())

	t.Run("set an whitespace value", func(t *testing.T) {
		assert.NotNil(t, val.Set("  "))
		assert.Equal(t, uuid.UUID{}.String(), val.String())
	})
}

func TestUUIDSliceValue(t *testing.T) {
	data := make([]uuid.UUID, 10)
	for i := range 10 {
		var err error
		data[i], err = uuid.NewRandom()
		require.Nil(t, err)
	}

	val := pflagtypes.NewUUIDSliceValue(data, '|')
	assert.Equal(t, "uuidSlice", val.Type())
	for i, v := range strings.Split(strings.Trim(val.String(), "[]"), "|") {
		assert.Equal(t, data[i].String(), v)
	}

	secondData := make([]uuid.UUID, 10)
	for i := range 10 {
		var err error
		secondData[i], err = uuid.NewRandom()
		require.Nil(t, err)
	}
	assert.Nil(t, val.Set(joinUUIDSlice(secondData, '|')))
	checkUUIDSlice(t, val, secondData, '|')

	t.Run("set an whitespace value", func(t *testing.T) {
		assert.Nil(t, val.Set("  "))
		assert.Equal(t, "[]", val.String())
	})

	t.Run("nil slice to constructor", func(t *testing.T) {
		val := pflagtypes.NewUUIDSliceValue(nil, ' ')
		assert.Equal(t, "uuidSlice", val.Type())
		assert.Equal(t, "[]", val.String())
		secondData := make([]string, 10)
		for i := range 10 {
			u, err := uuid.NewRandom()
			secondData[i] = u.String()
			require.Nil(t, err)
		}
		assert.Nil(t, val.Set(strings.Join(secondData, " ")))
		assert.Equal(t, strings.Split(strings.Trim(val.String(), "[]"), " "), secondData)
	})
	t.Run("bad parse in set of good values", func(t *testing.T) {
		val := pflagtypes.NewUUIDSliceValue(data, ',')
		r1, err := uuid.NewRandom()
		assert.Nil(t, err)
		r2, err := uuid.NewRandom()
		assert.Nil(t, err)
		badData := strings.Join([]string{r1.String(), "bad value", r2.String()}, ",")
		assert.NotNil(t, val.Set(badData))
		// ensure that the underlying slice was not affected
		checkUUIDSlice(t, val, data, ',')
	})
}

//#region UUIDSliceValue testing helpers

func joinUUIDSlice(uuids []uuid.UUID, sep rune) string {
	var strs = make([]string, len(uuids))
	for i, uuid := range uuids {
		strs[i] = uuid.String()
	}
	return strings.Join(strs, string(sep))
}

func checkUUIDSlice(t *testing.T, val *pflagtypes.UUIDSliceValue, expected []uuid.UUID, sep rune) {
	t.Helper()
	for i, v := range strings.Split(strings.Trim(val.String(), "[]"), string(sep)) {
		assert.Equal(t, expected[i].String(), v)
	}
}
