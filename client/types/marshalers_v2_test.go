package types_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/require"
)

func TestTDD(t *testing.T) {
	tests := []struct {
		name                  string
		data                  any
		includeOmitZeroOption bool // include the json.OmitZeroStructFields option when marshaling
		expected              string
	}{
		{"Optional field is included if set",
			struct{ S types.Optional[string] }{S: types.NewOptional("foo")},
			false,
			`{"S":"foo"}`,
		},
		{"Optional field with zero value is included",
			struct{ S types.Optional[int32] }{S: types.NewOptional[int32](5)},
			true,
			`{"S":5}`,
		},
		{"Optional field is omitted if omitzero tag is present",
			struct {
				S types.Optional[string] `json:",omitzero"`
			}{},
			false,
			`{}`,
		},
		{"Optional field is omitted if OmitZeroStructFields option is present",
			struct{ S types.Optional[int32] }{S: types.NewOptional[int32](5)},
			true,
			`{"S":5}`,
		},
		{"Zero Patch results in emptyJSON",
			types.CommonFieldsPatch{},
			true,
			`{}`,
		},
		{"Partially populated Patch type results in only populated fields",
			types.CommonFieldsPatch{
				Description: types.NewOptional(""),
				Writers:     types.NewOptional(types.ACL{GIDs: []int32{1, 2, 12}}),
			},
			true,
			`{"Description":"","Writers":{"GIDs":[1,2,12],"Global":false}}`,
		},
		{"Fully populated Patch type results in full JSO",
			types.CommonFieldsPatch{
				Description: types.NewOptional("desc"),
				Labels:      types.NewOptional([]string{"shí"}),
				Name:        types.NewOptional("fully popped"),
				OwnerID:     types.NewOptional[int32](1),
				Readers:     types.NewOptional(types.ACL{GIDs: []int32{0, 5, 8, 0}}),
				Writers:     types.NewOptional(types.ACL{}),
			},
			true,
			`{"Description":"desc","Labels":["shí"],"Name":"fully popped","OwnerID":1,"Readers":{"GIDs":[0,5,8,0],"Global":false},"Writers":{"GIDs":[],"Global":false}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []json.Options
			if tt.includeOmitZeroOption {
				opts = append(opts, json.OmitZeroStructFields(true))
			}
			b, err := json.Marshal(&tt.data, opts...)
			require.Nil(t, err, "failed to marshal %v", &tt.data)
			t.Logf("Marshaled %+v to %s", tt.data, b)
			require.Equal(t, tt.expected, string(b))
		})
	}

	t.Run("Complex Optional scenarios", func(t *testing.T) {
		t.Run("Nested T", func(t *testing.T) {
			// very messy, but a good way to ensure we capture all fields
			v := types.NewOptional([]struct {
				A int32
				B struct{ Yī, èr string }
			}{struct {
				A int32
				B struct {
					Yī string
					èr string
				}
			}{A: 5, B: struct {
				Yī string
				èr string
			}{"one", "two"}}})
			b, err := json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `[{"A":5,"B":{"Yī":"one"}}]`, string(b))

			v.Unset()
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			// we unset the whole thing, so the zero value ([]struct{...}(nil)) is marshaled;
			// JSON/v2 defaults nil slices to '[]' rather than null.
			require.Equal(t, `[]`, string(b))
		})
		t.Run("Set -> Set -> Unset -> Set", func(t *testing.T) {
			v := struct {
				A types.Optional[int32]
				B struct{ One, two types.Optional[string] }
				C types.Optional[struct {
					three bool
					Four  float32
				}]
			}{
				A: types.NewOptional[int32](0),
				B: struct {
					One types.Optional[string]
					two types.Optional[string]
				}{types.NewOptional("Yī"), types.NewOptional("èr")},
				C: types.NewOptional(struct {
					three bool
					Four  float32
				}{true, 4.4}),
			}
			// everything should be included
			b, err := json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `{"A":0,"B":{"One":"Yī"},"C":{"Four":4.4}}`, string(b))

			// set a couple new values
			v.B.One.Set("ein")
			v.B.two.Set("swei")
			// everything should be included
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `{"A":0,"B":{"One":"ein"},"C":{"Four":4.4}}`, string(b))

			// unset everything
			v.A.Unset()
			v.B.One.Unset()
			v.C.Unset()
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			// B is a concrete struct, so it will always be included.
			// This is intentional.
			require.Equal(t, `{"B":{}}`, string(b))
		})

	})
}
