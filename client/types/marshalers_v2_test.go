package types_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/require"
)

func TestOptionalPatch(t *testing.T) {
	t.Run("Optional is included if set", func(t *testing.T) {
		st := struct {
			S types.Optional[string]
		}{S: types.NewOptional("foo")}
		b, err := json.Marshal(&st)
		require.Nil(t, err)
		require.Equal(t, `{"S":"foo"}`, string(b))
	})
	t.Run("Optional is skipped if omitzero tag is included in field", func(t *testing.T) {
		st := struct {
			S types.Optional[string] `json:",omitzero"`
		}{}
		b, err := json.Marshal(&st)
		require.Nil(t, err)
		require.Equal(t, "{}", string(b))
	})
	t.Run("Optional is skipped if omitzero option is included in encoder", func(t *testing.T) {
		st := struct {
			S types.Optional[string]
		}{}
		b, err := json.Marshal(&st, json.OmitZeroStructFields(true))
		require.Nil(t, err)
		require.Equal(t, "{}", string(b))
	})

	t.Run("empty patch produces no values", func(t *testing.T) {
		m := types.MacroPatch{}
		//m.Expansion.Set("xp")
		b, err := json.Marshal(&m, json.OmitZeroStructFields(true))
		require.Nil(t, err)
		require.Equal(t, "{}", string(b))
	})
	/*t.Run("an set field is included", func(t *testing.T) {
		m := types.MacroPatch{Expansion: types.NewOptional("Venkmann")}
		m.Expansion.Unset()
		b, err := json.Marshal(&m)
		require.Nil(t, err)
		require.Empty(t, string(b))
		t.Run("an unset field is absent", func(t *testing.T) {
			m.Expansion.Unset()
			b, err := json.Marshal(&m)
			require.Nil(t, err)
			require.Empty(t, string(b))
		})
	})*/
}
