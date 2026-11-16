/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	"bytes"
	"encoding/gob"
	"encoding/json/v2"
	"fmt"
	"go/ast"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireNPState asserts n is in exactly one of the three states.
func requireNPState[T any](t *testing.T, n types.NullablePatchIn[T], omitted, null bool) {
	t.Helper()

	// sanity check that we never test for both omitted and null (should be an impossible state)
	assert.False(t, omitted && null,
		"NullablePatchIn should never be expected to be in two states at once")

	require.Equal(t, omitted, n.IsOmitted(), "IsOmitted")
	require.Equal(t, null, n.IsNull(), "IsNull")
	require.Equal(t, omitted, n.IsZero(), "IsZero must track IsOmitted")
}

func TestNullablePatchIn(t *testing.T) {
	t.Run("OmittedIsTheZeroState", func(t *testing.T) {
		var n types.NullablePatchIn[string]
		requireNPState(t, n, true, false)
		require.Equal(t, "", n.Value())
	})

	t.Run("ConstructorSetsStateToValue", func(t *testing.T) {
		n := types.NewNullablePatchIn("fresh hot ubik")
		requireNPState(t, n, false, false)
		require.Equal(t, "fresh hot ubik", n.Value())
	})

	t.Run("ZeroValueIsNeitherOmittedNorNull", func(t *testing.T) {
		n := types.NewNullablePatchIn("")
		requireNPState(t, n, false, false)
	})

	t.Run("TransitionBetweenStates", func(t *testing.T) {
		// value should be dropped whenever we change state

		var n types.NullablePatchIn[string]
		n.Set("vulcanized ubik")
		requireNPState(t, n, false, false)
		require.Equal(t, "vulcanized ubik", n.Value())
		require.Equal(t, n.Value(), n.String())

		n.Null()
		requireNPState(t, n, false, true)
		require.Equal(t, "", n.Value())
		require.Equal(t, "null", n.String())

		n.Set("got a headache? take ubik!")
		requireNPState(t, n, false, false)
		require.Equal(t, "got a headache? take ubik!", n.Value())
		require.Equal(t, n.Value(), n.String())

		n.Omit()
		requireNPState(t, n, true, false)
		require.Equal(t, "", n.Value())
		require.Empty(t, n.String())

		n.Null()
		requireNPState(t, n, false, true)
		require.Equal(t, "null", n.String())

		n.Omit()
		requireNPState(t, n, true, false)
		require.Empty(t, n.String())
	})

	t.Run("SatisfiesOpaqueValue", func(t *testing.T) {
		// weave checks the field's (non-pointer) type, so value receivers are required
		var n any = types.NullablePatchIn[string]{}
		ov, ok := n.(weave.OpaqueValue)
		require.True(t, ok)
		require.True(t, ov.IsOpaqueValue())
	})

	t.Run("Apply", func(t *testing.T) {
		t.Run("OmittedLeaves_dst_Untouched", func(t *testing.T) {
			dst := types.NewNullable("keep")
			var n types.NullablePatchIn[string]
			n.Apply(&dst)
			require.False(t, dst.IsNull())
			require.Equal(t, "keep", dst.Value())

			var nullDst types.Nullable[string]
			n.Apply(&nullDst)
			require.True(t, nullDst.IsNull())
		})
		t.Run("NullNulls_dst", func(t *testing.T) {
			dst := types.NewNullable("drop")
			var n types.NullablePatchIn[string]
			n.Null()
			n.Apply(&dst)
			require.True(t, dst.IsNull())
			require.Equal(t, "", dst.Value())
		})
		t.Run("ValueSets_dst", func(t *testing.T) {
			var dst types.Nullable[string]
			types.NewNullablePatchIn("new").Apply(&dst)
			require.False(t, dst.IsNull())
			require.Equal(t, "new", dst.Value())

			// a set zero value is still a value, not a null
			types.NewNullablePatchIn("").Apply(&dst)
			require.False(t, dst.IsNull())
			require.Equal(t, "", dst.Value())
		})
	})

	// Pins NullablePatchIn[T]'s stringification:
	// null prints "null",
	// omitted prints T's zero value,
	// and a value prints its own string form (which may coincide with the zero value).
	t.Run("String", func(t *testing.T) {
		var n types.NullablePatchIn[string]
		require.Equal(t, "", n.String())

		n.Set("biologist")
		require.Equal(t, "biologist", n.String())

		n.Null()
		require.Equal(t, "null", n.String())

		n.Omit()
		require.Equal(t, "", n.String())

		ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
		require.Equal(t, ts.String(), types.NewNullablePatchIn(ts).String())

		// %v on a struct holding n must print n.String(), not its priv fields
		s := struct{ N types.NullablePatchIn[string] }{N: types.NewNullablePatchIn("x")}
		require.Equal(t, "{x}", fmt.Sprintf("%v", s))
	})

	t.Run("Equal", func(t *testing.T) {
		var a, b types.NullablePatchIn[string]
		require.True(t, a.Equal(b), "two omitted values should be equal")

		a.Null()
		require.False(t, a.Equal(b), "null should not equal omitted")

		b.Null()
		require.True(t, a.Equal(b), "two nulls should be equal")

		a.Set("")
		require.False(t, a.Equal(b), "a set zero value should not equal null")

		b.Omit()
		require.False(t, a.Equal(b), "a set zero value should not equal omitted")

		b.Set("")
		require.True(t, a.Equal(b), "two sets holding the same value should be equal")

		b.Set("y")
		require.False(t, a.Equal(b), "two sets holding different values should not be equal")
	})

	t.Run("Unmarshal", func(t *testing.T) {
		type patch struct {
			A types.NullablePatchIn[string]
			B types.NullablePatchIn[int32]
			C types.NullablePatchIn[map[string]int]
		}

		t.Run("AbsentFieldsCauseOmittedState", func(t *testing.T) {
			var p patch
			require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
			requireNPState(t, p.A, true, false)
			requireNPState(t, p.B, true, false)
			requireNPState(t, p.C, true, false)
		})

		// null must be consumed from the decoder; otherwise the decode fails outright
		// (or desyncs from any sibling fields that follow it).
		t.Run("NullFieldsCauseNullState", func(t *testing.T) {
			var p patch
			require.NoError(t, json.Unmarshal([]byte(`{"A":null,"B":null,"C":null}`), &p))
			requireNPState(t, p.A, false, true)
			requireNPState(t, p.B, false, true)
			requireNPState(t, p.C, false, true)
		})

		t.Run("ValuesSet", func(t *testing.T) {
			var p patch
			require.NoError(t, json.Unmarshal([]byte(`{"A":"x","B":5,"C":{"a":1}}`), &p))
			requireNPState(t, p.A, false, false)
			require.Equal(t, "x", p.A.Value())
			require.Equal(t, int32(5), p.B.Value())
			require.Equal(t, map[string]int{"a": 1}, p.C.Value())
		})

		t.Run("ZeroValuesSet", func(t *testing.T) {
			var p patch
			require.NoError(t, json.Unmarshal([]byte(`{"A":"","B":0,"C":{}}`), &p))
			requireNPState(t, p.A, false, false)
			requireNPState(t, p.B, false, false)
			requireNPState(t, p.C, false, false)
			require.Equal(t, map[string]int{}, p.C.Value())
		})

		t.Run("MixedState", func(t *testing.T) {
			var p patch
			require.NoError(t, json.Unmarshal([]byte(`{"A":null,"B":7}`), &p))
			requireNPState(t, p.A, false, true)
			requireNPState(t, p.B, false, false)
			require.Equal(t, int32(7), p.B.Value())
			requireNPState(t, p.C, true, false)
		})

		t.Run("ValuesDoNotMerge", func(t *testing.T) {
			// collection types (maps/arrays) should not merge during the unmarshal!
			// The new value should supplant.
			n := types.NewNullablePatchIn(map[string]int{"a": 1})
			require.NoError(t, json.Unmarshal([]byte(`{"b":2}`), &n))
			require.Equal(t, map[string]int{"b": 2}, n.Value())
		})

		t.Run("NullOverridesPriorValue", func(t *testing.T) {
			n := types.NewNullablePatchIn("x")
			require.NoError(t, json.Unmarshal([]byte(`null`), &n))
			requireNPState(t, n, false, true)
			require.Equal(t, "", n.Value())
		})

		t.Run("BadValueErrors", func(t *testing.T) {
			// an error should leave the prior value alone
			n := types.NewNullablePatchIn[int32](3)
			require.Error(t, json.Unmarshal([]byte(`"not a number"`), &n))
			requireNPState(t, n, false, false)
			require.Equal(t, int32(3), n.Value())

			var o types.NullablePatchIn[int32]
			require.Error(t, json.Unmarshal([]byte(`"not a number"`), &o))
			requireNPState(t, o, true, false)
		})
	})

	t.Run("Marshal", func(t *testing.T) {
		null := func() types.NullablePatchIn[string] {
			var n types.NullablePatchIn[string]
			n.Null()
			return n
		}

		type tagged struct {
			S types.NullablePatchIn[string] `json:",omitzero"`
		}
		type untagged struct {
			S types.NullablePatchIn[string]
		}

		// omitted is the only state that may error; nothing else depends on tags
		tests := []struct {
			name                  string
			data                  any
			includeOmitZeroOption bool   // include the json.OmitZeroStructFields option when marshaling
			expected              string // ignored if expectErr
			expectErr             bool   // expect ErrOmittedNullablePatchIn
		}{
			{"omitted is skipped with omitzero tag", tagged{}, false, `{}`, false},
			{"omitted is skipped with OmitZeroStructFields", untagged{}, true, `{}`, false},
			{"omitted errors without omitzero", untagged{}, false, "", true},
			{"null marshals to null with omitzero tag", tagged{S: null()}, false, `{"S":null}`, false},
			{"null marshals to null with OmitZeroStructFields",
				untagged{S: null()}, true, `{"S":null}`, false},
			{"null marshals to null without omitzero", untagged{S: null()}, false, `{"S":null}`, false},
			{"value marshals to value",
				tagged{S: types.NewNullablePatchIn("x")}, false, `{"S":"x"}`, false},
			{"value marshals to value without omitzero",
				untagged{S: types.NewNullablePatchIn("x")}, false, `{"S":"x"}`, false},
			{"set zero value is not omitted",
				tagged{S: types.NewNullablePatchIn("")}, false, `{"S":""}`, false},
			{"set zero value is not omitted by OmitZeroStructFields",
				untagged{S: types.NewNullablePatchIn("")}, true, `{"S":""}`, false},
			{"bare omitted errors", types.NullablePatchIn[[]string]{}, false, "", true},
			{"omitted slice element errors",
				[]types.NullablePatchIn[string]{types.NewNullablePatchIn("x"), {}}, true, "", true},
			{"bare null marshals to null", null(), false, `null`, false},
			{"bare value marshals to value",
				types.NewNullablePatchIn([]string{"a"}), false, `["a"]`, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var opts []json.Options
				if tt.includeOmitZeroOption {
					opts = append(opts, json.OmitZeroStructFields(true))
				}
				// marshal by value to ensure nothing depends on addressability
				b, err := json.Marshal(tt.data, opts...)
				if tt.expectErr {
					require.ErrorIs(t, err, types.ErrOmittedNullablePatchIn)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.expected, string(b))
			})
		}

		// Edge case we must guarantee:
		// a zero time.Time and null are both meaningful and must stay distinct.
		t.Run("TimeRoundTrips", func(t *testing.T) {
			type patch struct {
				Omitted types.NullablePatchIn[time.Time] `json:",omitzero"`
				Null    types.NullablePatchIn[time.Time] `json:",omitzero"`
				Zero    types.NullablePatchIn[time.Time] `json:",omitzero"`
				Value   types.NullablePatchIn[time.Time] `json:",omitzero"`
			}
			ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
			in := patch{Zero: types.NewNullablePatchIn(time.Time{}), Value: types.NewNullablePatchIn(ts)}
			in.Null.Null()

			b, err := json.Marshal(in)
			require.NoError(t, err)
			require.Equal(t,
				`{"Null":null,"Zero":"0001-01-01T00:00:00Z","Value":"2026-09-17T12:00:00Z"}`,
				string(b))

			var out patch
			require.NoError(t, json.Unmarshal(b, &out))
			requireNPState(t, out.Omitted, true, false)
			requireNPState(t, out.Null, false, true)
			requireNPState(t, out.Zero, false, false)
			require.True(t, out.Zero.Value().IsZero())
			requireNPState(t, out.Value, false, false)
			require.True(t, out.Value.Value().Equal(ts))
		})
	})
}

// isNullablePatchInExpr reports if expr is NullablePatchIn[T], pkg.NullablePatchIn[T],
// or a pointer to either.
func isNullablePatchInExpr(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	idx, ok := expr.(*ast.IndexExpr)
	if !ok {
		return false
	}
	switch x := idx.X.(type) {
	case *ast.Ident:
		return x.Name == "NullablePatchIn"
	case *ast.SelectorExpr:
		return x.Sel.Name == "NullablePatchIn"
	}
	return false
}

func TestNullablePatchInGobRoundTrip(t *testing.T) {
	roundTrip := func(t *testing.T, in types.NullablePatchIn[time.Time]) types.NullablePatchIn[time.Time] {
		t.Helper()
		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out types.NullablePatchIn[time.Time]
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))
		return out
	}
	ts := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)

	t.Run("Omitted", func(t *testing.T) {
		out := roundTrip(t, types.NullablePatchIn[time.Time]{})
		requireNPState(t, out, true, false)
	})

	t.Run("Null", func(t *testing.T) {
		var n types.NullablePatchIn[time.Time]
		n.Null()
		out := roundTrip(t, n)
		requireNPState(t, out, false, true)
	})

	t.Run("Set", func(t *testing.T) {
		out := roundTrip(t, types.NewNullablePatchIn(ts))
		requireNPState(t, out, false, false)
		require.True(t, out.Value().Equal(ts))
	})

	t.Run("EmbeddedInAStruct", func(t *testing.T) {
		type wrapper struct {
			Name      string
			Timeframe types.NullablePatchIn[time.Time]
			Cleared   types.NullablePatchIn[string]
		}
		in := wrapper{Name: "patch", Timeframe: types.NewNullablePatchIn(ts)}
		in.Cleared.Null()

		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(in))
		var out wrapper
		require.NoError(t, gob.NewDecoder(&buf).Decode(&out))

		require.Equal(t, "patch", out.Name)
		require.True(t, out.Timeframe.Value().Equal(ts))
		requireNPState(t, out.Cleared, false, true)
	})
}
