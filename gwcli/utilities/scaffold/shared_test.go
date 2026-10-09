package scaffold_test

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/stretchr/testify/require"
)

func TestFromString(t *testing.T) {
	tfs(t, "uuid", "1e16d1e9-4545-495d-9995-5d58ef4dcb68", uuid.MustParse("1e16d1e9-4545-495d-9995-5d58ef4dcb68"), false)
	tfs(t, "uint", "18446744073709551615", uint(math.MaxUint), false)
	tfs(t, "uint8", "255", uint8(math.MaxUint8), false)
	tfs(t, "uint16", "65535", uint16(math.MaxUint16), false)
	tfs(t, "uint32", "60", uint32(60), false)
	tfs(t, "uint64", "60", uint64(60), false)
	tfs(t, "int", "9223372036854775807", math.MaxInt, false)
	tfs(t, "-int", "-9223372036854775808", int(math.MinInt), false)
	tfs(t, "int8", "127", int8(math.MaxInt8), false)
	tfs(t, "int16", "3", int16(3), false)
	tfs(t, "int32", "60", int32(60), false)
	tfs(t, "int64", "60", int64(60), false)

	tfs(t, "uuid invalid", "1e16d1e9-4545-495d-9995", uuid.UUID{}, true)
	tfs(t, "int16 out of range", "65535", int16(math.MaxInt16), true)
	tfs(t, "bad character", "60s", 0, true)
	tfs(t, "empty", "", 0, true)
}

// helper for TestFromString to execute FromString and check the outcome.
func tfs[I scaffold.Id_t](t *testing.T, name string, strVal string, expected I, wantErr bool) {
	t.Run(name, func(t *testing.T) {
		out, err := scaffold.FromString[I](strVal)
		if wantErr {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, expected, out)
	})
}
