package weave

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

type too struct {
	mu int
	yu int16
}

type inner struct {
	foo string
	too *too
}

type outer struct {
	inner
	a        int
	b        uint
	c        *float32
	d        string
	Exported float64
}

func TestToCSV(t *testing.T) {
	const longCSVLineCount = 17000

	type args struct {
		st      []interface{}
		columns []string
	}

	var c float32 = 5.0123

	tests := []struct {
		name string
		args args
		want string
	}{
		{"∃!c∃!r",
			args{
				st: []interface{}{
					outer{
						a:     10,
						b:     0,
						c:     &c,
						d:     "D",
						inner: inner{foo: "FOO"}}},
				columns: []string{"a"}},
			"a\n" + "10",
		},
		{"∃c∃!r",
			args{
				st: []interface{}{
					outer{
						a:     10,
						b:     0,
						c:     &c,
						d:     "D",
						inner: inner{foo: "FOO"}}},
				columns: []string{"a", "c"}},
			"a,c\n" + "10,5.0123",
		},
		{"too ∀c2r, ordered as struct",
			args{
				st: []interface{}{
					too{mu: 1, yu: 2}, too{mu: 3, yu: 4}},
				columns: []string{
					"mu", "yu",
				}},
			"mu,yu\n" + "1,2\n" + "3,4",
		},
		{"∃!c∃!r, deeply nested",
			args{
				st: []interface{}{
					outer{inner: inner{too: &too{mu: 5}}},
				},
				columns: []string{
					"too.mu",
				}},
			"too.mu\n" + "5",
		},
		{"∃c∃!r, deeply nested",
			args{
				st: []interface{}{
					outer{inner: inner{too: &too{mu: 5, yu: 6}}},
				},
				columns: []string{
					"too.mu", "too.yu",
				}},
			"too.mu,too.yu\n" + "5,6",
		},
		{"∃c∃!r, deeply nested",
			args{
				st: []interface{}{
					outer{inner: inner{too: &too{mu: 5, yu: 6}}, a: 10000, Exported: -87.5},
				},
				columns: []string{
					"Exported", "too.mu", "too.yu", "a",
				}},
			"Exported,too.mu,too.yu,a\n" + "-87.5,5,6,10000",
		},
		{"∀c∃!r, ordered as struct",
			args{
				st: []interface{}{
					outer{
						inner:    inner{foo: "FOO", too: &too{mu: 5, yu: 1}},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145}},
				columns: []string{
					"foo", "too.mu", "too.yu", "a", "b", "c", "d", "Exported", "too.mu",
				}},
			"foo,too.mu,too.yu,a,b,c,d,Exported,too.mu\n" + "FOO,5,1,10,0,5.0123,D,3.145,5",
		},
		{"∀c∃!r, ordered randomly",
			args{
				st: []interface{}{
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145}},
				columns: []string{
					"c", "foo", "Exported", "d", "a", "b",
				}},
			"c,foo,Exported,d,a,b\n" + "5.0123,FOO,3.145,D,10,0",
		},
		{"∀c5r, ordered randomly",
			args{
				st: []interface{}{
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        57,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        256,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D!",
						Exported: 3.145}},
				columns: []string{
					"c", "foo", "Exported", "d", "a", "b",
				}},
			"c,foo,Exported,d,a,b\n" +
				"5.0123,FOO,3.145,D,10,0\n" +
				"5.0123,FOO,3.145,D,57,0\n" +
				"5.0123,FOO,3.145,D,10,256\n" +
				"5.0123,FOO,3.145,D,10,0\n" +
				"5.0123,FOO,3.145,D!,10,0",
		},
		{"∃c2r, non-existant column 'missing' and 'foobar'",
			args{
				st: []interface{}{
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145}},
				columns: []string{
					"c", "foo", "Exported", "missing", "d", "a", "b", "foobar",
				}},
			"c,foo,Exported,missing,d,a,b,foobar\n" + "5.0123,FOO,3.145,,D,10,0,\n" + "5.0123,FOO,3.145,,D,10,0,",
		},
		{"superfluous, no columns",
			args{
				st: []interface{}{
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145},
					outer{
						inner:    inner{foo: "FOO"},
						a:        10,
						b:        0,
						c:        &c,
						d:        "D",
						Exported: 3.145}},
				columns: []string{}},
			"",
		},
		{"superfluous, no data",
			args{
				st:      []interface{}{},
				columns: []string{"c", "foo", "Exported", "missing", "d", "a", "b", "foobar"}},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToCSV(tt.args.st, tt.args.columns); got != tt.want {
				t.Errorf("\n---ToCSVHash()---\n'%v'\n---want---\n'%v'", got, tt.want)
			}
		})
	}

	t.Run("not a struct", func(t *testing.T) {
		m := map[int]float32{}

		if got := ToCSV([]map[int]float32{m}, []string{"some", "column", "names"}); got != "" {
			t.Errorf("expected the empty string, got %v", got)
		}
	})

	// test against significant amounts of data
	t.Run("long data", func(t *testing.T) {
		// set up the data and structures
		type innerInnerInnerNest struct {
			iiin string
		}
		type innerInnerNest struct {
			innerInnerInnerNest
			iin string
		}
		type innerNest struct {
			innerInnerNest
			in string
		}
		type nest struct {
			innerNest
			n string
		}

		var data []nest = make([]nest, longCSVLineCount)
		for i := 0; i < longCSVLineCount; i++ {
			data[i] = nest{
				n: fmt.Sprintf("%dN", i), innerNest: innerNest{
					in: "IN", innerInnerNest: innerInnerNest{
						iin: "IIN", innerInnerInnerNest: innerInnerInnerNest{iiin: "IIIN"},
					},
				},
			}
		}

		var expectedBldr strings.Builder
		expectedBldr.Grow(longCSVLineCount * 16)    // roughly 14-16B per line; better overallocate
		expectedBldr.WriteString("n,in,iin,iiin\n") // header
		for i := 0; i < longCSVLineCount; i++ {
			expectedBldr.WriteString(
				fmt.Sprintf("%dN,IN,IIN,IIIN\n", i),
			)
		}

		actual := ToCSV(data, []string{"n", "in", "iin", "iiin"})
		expected := strings.TrimSpace(expectedBldr.String()) // chomp newline
		if actual != expected {
			// count newlines in parallel
			actualCountDone := make(chan int)
			var actualCount int
			// check line length
			go func() {
				actualCountDone <- strings.Count(actual, "\n")
				close(actualCountDone)
			}()

			expectedCountDone := make(chan int)
			var expectedCount int
			go func() {
				expectedCountDone <- strings.Count(expected, "\n")
				close(expectedCountDone)
			}()

			// wait for children
			actualCount = <-actualCountDone
			expectedCount = <-expectedCountDone

			if actualCount != expectedCount {
				t.Errorf("# of lines in actual (%d) <> # of lines in expected (%d)", actualCount, expectedCount)
			}

			t.Errorf("actual does not match expected!\n---actual---\n%s\n---expected---\n%s\n", actual, expected)
		}
	})

	t.Run("ptr", func(t *testing.T) {
		// define struct with pointer
		type ptrstruct struct {
			a   int
			ptr *int
		}

		ptrval := 5
		st := ptrstruct{
			a:   1,
			ptr: &ptrval,
		}

		want := "a,ptr\n" +
			"1,5"

		actual := ToCSV([]ptrstruct{st}, []string{"a", "ptr"})

		if actual != want {
			t.Errorf("\n---ToCSVHash()---\n'%v'\n---want---\n'%v'", actual, want)
		}

	})

	// nested pointers
	type ptrstruct struct {
		a int
		b string
	}
	type inner struct {
		inptr *int
		p     *ptrstruct
	}
	type outer struct {
		inner
		z uint
	}

	t.Run("embedded pointers, all initialized", func(t *testing.T) {
		inptrVal := -9
		ptrStructVal := ptrstruct{a: 0, b: "B"}
		v := outer{z: 10, inner: inner{inptr: &inptrVal, p: &ptrStructVal}}
		actual := ToCSV([]outer{v}, []string{"z", "inptr", "p", "a", "b"})
		expected := "z,inptr,p,a,b\n" +
			"10,-9,{0 B},,"
		if actual != expected {
			t.Errorf("\n---ToCSVHash()---\n'%v'\n---want---\n'%v'", actual, expected)
		}
	})
}

func TestToTable(t *testing.T) {
	t.Run("superfluous", func(t *testing.T) {
		actual := ToTable[any](nil, []string{"A", "B", "c"})

		if actual != "" {
			t.Errorf("string mismatch.\nactual%s\nexpected the empty string", actual)
		}
	})

	type d1 struct {
		one string
		Two string
	}
	type d0 struct {
		A      int
		B      int
		c      string
		depth1 d1
	}

	t.Run("depth 0, all columns", func(t *testing.T) {
		actualData := []d0{
			{A: 1, B: 2, c: "c"},
			{A: 1, B: 2, c: "c"},
		}

		expectedRows := [][]string{
			{"1", "2", "c"},
			{"1", "2", "c"},
		}
		expectedHeader := []string{"A", "B", "c"}

		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		actual := ToTable(actualData, []string{"A", "B", "c"})
		if actual != expected {
			t.Errorf("string mismatch.\nactual%s\nexpected%s", actual, expected)
		}
	})
	t.Run("depth 1, all columns", func(t *testing.T) {
		actualData := []d0{
			{A: 1, B: 2, c: "c", depth1: d1{one: "one", Two: "Two"}},
			{A: 1, B: 2, c: "c", depth1: d1{one: "one", Two: "Two"}},
		}
		actual := ToTable(actualData, []string{"A", "B", "c", "depth1.one", "depth1.Two"})

		expectedRows := [][]string{
			{"1", "2", "c", "one", "Two"},
			{"1", "2", "c", "one", "Two"},
		}
		expectedHeader := []string{"A", "B", "c", "depth1.one", "depth1.Two"}
		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})
	t.Run("depth 1, some columns", func(t *testing.T) {
		actualData := []d0{
			{A: 1, B: 2, c: "c", depth1: d1{one: "one", Two: "Two"}},
			{A: 1, B: 2, c: "c", depth1: d1{one: "one", Two: "Two"}},
		}
		actual := ToTable(actualData, []string{"A", "depth1.one", "depth1.Two"})

		expectedRows := [][]string{
			{"1", "one", "Two"},
			{"1", "one", "Two"},
		}
		expectedHeader := []string{"A", "depth1.one", "depth1.Two"}
		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})
	t.Run("depth 1, some columns, varying data", func(t *testing.T) {
		actualData := []d0{
			{A: 1, B: 2, c: "c", depth1: d1{one: "one", Two: "Two"}},
			{A: 3, B: 4, c: "c2", depth1: d1{one: "one2", Two: "Two2"}},
		}
		actual := ToTable(actualData, []string{"A", "depth1.one", "depth1.Two"})

		expectedRows := [][]string{
			{"1", "one", "Two"},
			{"3", "one2", "Two2"},
		}
		expectedHeader := []string{"A", "depth1.one", "depth1.Two"}
		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})

	type e1 struct {
		Alpha *float32
		beta  float64
	}
	type d1p struct {
		e1
		one *string
	}
	type d0p struct {
		A       *int
		B       int
		c       *string
		D       string
		depth1p *d1p
	}

	t.Run("depth 0, w/ pointers and all columns", func(t *testing.T) {
		A, c := 1, "c"

		actualData := []d0p{
			{A: &A, B: 2, c: &c},
			{A: &A, B: 2, c: &c},
		}
		actual := ToTable(actualData, []string{"A", "B", "c"})

		expectedRows := [][]string{
			{"1", "2", "c"},
			{"1", "2", "c"},
		}
		expectedHeader := []string{"A", "B", "c"}
		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})

	t.Run("depth 1, w/ pointers, embed, and all columns", func(t *testing.T) {
		// data for pointers
		A, c, one := 1, "c", "one"
		var Alpha float32 = 3.14
		depth1p := d1p{e1: e1{Alpha: &Alpha, beta: 6.28}, one: &one}

		actualData := []d0p{
			{A: &A, B: 2, c: &c, D: "D", depth1p: &depth1p},
			{A: &A, B: 2, c: &c, D: "D", depth1p: &depth1p},
		}
		actual := ToTable(actualData, []string{"A", "B", "c", "D", "depth1p.Alpha", "depth1p.beta", "depth1p.one"})

		expectedRows := [][]string{
			{"1", "2", "c", "D", "3.14", "6.28", "one"},
			{"1", "2", "c", "D", "3.14", "6.28", "one"},
		}
		expectedHeader := []string{"A", "B", "c", "D", "depth1p.Alpha", "depth1p.beta", "depth1p.one"}
		expected := DefaultTblStyle().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})

	t.Run("depth 1, w/ pointers, embed, custom style, and all columns", func(t *testing.T) {
		styleFunc := func() *table.Table {
			return table.New().Border(lipgloss.OuterHalfBlockBorder()).StyleFunc(func(row, col int) lipgloss.Style {
				return lipgloss.NewStyle().Width(5).Foreground(lipgloss.Color("#AABBCC")) // set set row and column width
			})
		}

		// data for pointers
		A, c, one := 1, "c", "one"
		var Alpha float32 = 3.14
		depth1p := d1p{e1: e1{Alpha: &Alpha, beta: 6.28}, one: &one}

		actualData := []d0p{
			{A: &A, B: 2, c: &c, D: "D", depth1p: &depth1p},
			{A: &A, B: 2, c: &c, D: "D", depth1p: &depth1p},
		}
		actual := ToTable(actualData,
			[]string{"A", "B", "c", "D", "depth1p.Alpha", "depth1p.beta", "depth1p.one"},
			styleFunc)

		expectedRows := [][]string{
			{"1", "2", "c", "D", "3.14", "6.28", "one"},
			{"1", "2", "c", "D", "3.14", "6.28", "one"},
		}
		expectedHeader := []string{"A", "B", "c", "D", "depth1p.Alpha", "depth1p.beta", "depth1p.one"}
		expected := styleFunc().Headers(expectedHeader...).Rows(expectedRows...).Render()

		if actual != expected {
			t.Errorf("string mismatch.\nactual\n%s\nexpected\n%s", actual, expected)
		}
	})
}

func TestToJSON(t *testing.T) {
	t.Run("superfluous", func(t *testing.T) {
		var err error
		var a1, a2 string
		a1, err = ToJSON[any](nil, []string{"A", "B", "c"})
		if err != nil {
			t.Error("Expected no error, got: ", err)
		}
		a2, err = ToJSON[any]([]interface{}{}, nil)
		if err != nil {
			t.Error("Expected no error, got: ", err)
		}

		if a1 != "[]" || a2 != "[]" {
			t.Errorf("expected '[]', got (a1:%v) (a2:%v)", a1, a2)
		}
	})

	t.Run("depth 0 simple", func(t *testing.T) {
		type d0 struct {
			A int
			b int
			C string
		}
		data := []d0{
			{A: 1, b: -2, C: "C string"},
		}

		actual, err := ToJSON(data, []string{"A", "C"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})

	t.Run("depth 0 all string pointers", func(t *testing.T) {
		type d0 struct {
			A *string
			b *string
			C *string
		}
		A, b, C := "1.0", "-2", "C string"
		data := []d0{
			{A: &A, b: &b, C: &C},
		}

		actual, err := ToJSON(data, []string{"A", "C"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})
	t.Run("depth 0 various types", func(t *testing.T) {
		type d0 struct {
			A *string
			b *float64
			B *float64
			c *float32
			C *float32
			D uint
			E uint8
			F uint16
			G uint32
			H uint64
		}
		A, b, B := "1.0", 1.1, 2.2
		var c, C float32 = 3.3, 4.4
		var D uint = 0
		var E uint8 = 1
		var F uint16 = 2
		var G uint32 = 3
		var H uint64 = 4
		data := []d0{
			{A: &A, b: &b, B: &B, c: &c, C: &C,
				D: D, E: E, F: F, G: G, H: H},
		}

		actual, err := ToJSON(data, []string{"A", "B", "C",
			"D", "E", "F", "G", "H"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})
	t.Run("depth 0 arrays", func(t *testing.T) {
		type d0 struct {
			A *[]string
			B []int
		}
		A := []string{"1", "2", "3"}
		B := []int{1, 2, 3}
		data := []d0{
			{A: &A, B: B},
		}

		actual, err := ToJSON(data, []string{"A", "B"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})

	// NOTE: this test just checks that an error did not occur in parsing; it
	// does not compare the result to standard JSON encoding, as the encoder
	// cannot handle complex numbers
	t.Run("depth 0 complex", func(t *testing.T) {
		type d0 struct {
			A complex64
			B *complex128
		}
		var A complex64 = 10 + 11i
		B := complex(1.5, 8.5)
		data := []d0{
			{A: A, B: &B},
		}

		actual, err := ToJSON(data, []string{"A", "B"})
		if err != nil {
			panic(err)
		}

		/* want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"*/

		want := "[{\"A\":{\"Real\":10,\"Imaginary\":11},\"B\":{\"Real\":1.5,\"Imaginary\":8.5}}]"

		if want != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", want, actual)
		}
	})
	t.Run("r5 depth 0 complex", func(t *testing.T) {
		type d0 struct {
			A int
			B *uint
			c string
		}
		A, B := -5, uint(1)
		data := []d0{
			{A: A, B: &B, c: "My"},
			{A: A, B: &B, c: "cat"},
			{A: A, B: &B, c: "has"},
			{A: A, B: &B, c: "a"},
			{A: A, B: &B, c: "funny"},
			{A: A, B: &B, c: "face"},
		}

		actual, err := ToJSON(data, []string{"A", "B"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if want != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", "", actual)
		}
	})
	t.Run("depth 0 unexported panic", func(t *testing.T) {
		type d0 struct {
			A int
			B *uint
			c string
		}
		A, B := -5, uint(1)
		data := []d0{
			{A: A, B: &B, c: "Linux or death"},
		}

		defer func() {
			recover()
		}()
		ToJSON(data, []string{"A", "B", "c"})
		t.Error("ToJSON should have panicked due to unexported value")
	})
	t.Run("depth 1 simple", func(t *testing.T) {
		type d1 struct {
			A1 int
			B1 *float64
		}
		type d0 struct {
			A0     *string
			B0     string
			Depth1 d1
		}
		var A0 string = "Coffee or death"
		var B0 string = "donate to simply cats"
		var A1 int = 5
		var B1 float64 = 3.14
		data := []d0{
			{A0: &A0, B0: B0, Depth1: d1{A1: A1, B1: &B1}},
		}

		actual, err := ToJSON(data, []string{"A0", "B0", "Depth1.A1", "Depth1.B1"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})
	// NOTE: this test must be crafted carefully because ToJSON and
	// encoding/json do not use the same sorting method.
	//
	// encoding/json sorts by struct order
	// ToJSON (via gabs) sorts alphabetically
	t.Run("r30 deep ints", func(t *testing.T) {
		const iterations = 30
		type d3 struct {
			A uint8
			B uint16
			C uint32
			D *uint64
		}
		type d2 struct {
			A2     *int64
			B2     int32
			Depth3 d3
		}
		type d1 struct {
			A1     int16
			Depth2 *d2
		}
		type d0 struct {
			A0     int8
			Depth1 d1
		}
		var (
			d3A  uint8  = math.MaxUint8
			d3B  uint16 = math.MaxUint16
			d3C  uint32 = math.MaxUint32
			d3D  uint64 = math.MaxUint64
			d2A2 int64  = math.MaxInt64
			d2B2 int32  = math.MaxInt32
			d1A1 int16  = math.MaxInt16
			d0A0 int8   = math.MaxInt8
		)

		data := make([]d0, iterations)
		for i := 0; i < iterations; i++ {
			data[i] = d0{A0: d0A0,
				Depth1: d1{A1: d1A1,
					Depth2: &d2{A2: &d2A2, B2: d2B2,
						Depth3: d3{A: d3A, B: d3B, C: d3C, D: &d3D}}}}
		}

		actual, err := ToJSON(data, []string{"A0",
			"Depth1.A1",
			"Depth1.Depth2.A2", "Depth1.Depth2.B2",
			"Depth1.Depth2.Depth3.A", "Depth1.Depth2.Depth3.B", "Depth1.Depth2.Depth3.C", "Depth1.Depth2.Depth3.D"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if want != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})
	// NOTE: this test must be crafted carefully because ToJSON and
	// encoding/json do not use the same sorting method.
	//
	// encoding/json sorts by struct order
	// ToJSON (via gabs) sorts alphabetically
	t.Run("depth 1 embedding", func(t *testing.T) {
		type d1 struct {
			C1 int
			D1 *float64
		}
		type d0 struct {
			A0 *string
			B0 float32
			d1
		}
		var A0 string = "Go drink some water"
		var B0 float32 = 12.8
		var C1 int = 5
		var D1 float64 = 3.14
		data := []d0{
			{A0: &A0, B0: B0, d1: d1{C1: C1, D1: &D1}},
		}

		actual, err := ToJSON(data, []string{"A0", "B0", "C1", "D1"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})
	t.Run("depth 1 embedding w/ disordered columns", func(t *testing.T) {
		type d1 struct {
			C1 int
			D1 *float64
		}
		type d0 struct {
			A0 *string
			B0 float32
			d1
		}
		var A0 string = "Go drink some water"
		var B0 float32 = 12.8
		var C1 int = 5
		var D1 float64 = 3.14
		data := []d0{
			{A0: &A0, B0: B0, d1: d1{C1: C1, D1: &D1}},
		}

		actual, err := ToJSON(data, []string{"C1", "D1", "B0", "A0"})
		if err != nil {
			panic(err)
		}

		var want string = "["
		for _, d := range data {
			w, err := json.Marshal(d)
			if err != nil {
				panic(err)
			}
			want += string(w) + ","
		}
		want = strings.TrimSuffix(want, ",")
		want += "]"

		if string(want) != actual {
			t.Errorf("want <> actual:\nwant: '%v'\nactual: '%v'\n", string(want), actual)
		}
	})

}

func TestToJSONExclude(t *testing.T) {
	const emptyJSON = "[]"

	type d0 struct {
		a int
		B uint
		C string
		D *string
	}
	D := "D string"

	type args struct {
		st        []interface{}
		blacklist []string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{"superfluous", args{st: nil, blacklist: []string{"Ted", "Cruz", "ate", "my", "son"}}, emptyJSON, true},
		{"not a struct", args{st: []interface{}{[]map[uint]int{}}, blacklist: []string{"AI", "is", "a", "fad"}}, emptyJSON, true},
		{"∀c2r, nil blacklist, some values initialized",
			args{
				st: []interface{}{
					d0{a: 10, B: 0},
					d0{a: 11, B: 1},
				},
				blacklist: nil,
			},
			"[{\"B\":0,\"C\":\"\",\"D\":null},{\"B\":1,\"C\":\"\",\"D\":null}]",
			false,
		},
		{"∀c2r nil blacklist, all values initialized",
			args{
				st: []interface{}{
					d0{a: 10, B: 0, C: "C string", D: &D},
					d0{a: 11, B: 1, C: "C string", D: &D},
				},
				blacklist: nil,
			},
			"[{\"B\":0,\"C\":\"C string\",\"D\":\"" + D + "\"},{\"B\":1,\"C\":\"C string\",\"D\":\"" + D + "\"}]",
			false,
		},
		/*{"∀c2r blacklist B and C",
			args{
				st: []interface{}{
					d0{a: 10, B: 0, C: "C string", D: &D},
					d0{a: 11, B: 1, C: "C string", D: &D},
				},
				blacklist: []string{"B", "C"},
			},
			"[{\"D\":\"" + D + "\"},{\"D\":\"" + D + "\"}]",
			false,
		},*/
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToJSONExclude(tt.args.st, tt.args.blacklist)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToJSONExclude() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ToJSONExclude() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindQualifiedField(t *testing.T) {
	type lvl3 struct {
		d int
		e struct {
			a string
			b string
		}
	}
	// strutures to test on
	type lvl2 struct {
		b  uint
		c  *string
		l3 *lvl3
	}
	type lvl1 struct {
		lvl2
		l2 lvl2
		a  string
	}

	// silence "unused" warnings as we only care about types
	c := "c"
	var _ lvl1 = lvl1{l2: lvl2{b: 0, c: &c, l3: &lvl3{d: -8,
		e: struct {
			a string
			b string
		}{a: "", b: ""}}}, lvl2: lvl2{b: 9}, a: "a"}

	t.Run("depth 0", func(t *testing.T) {
		exp, expFound := reflect.TypeOf(lvl1{}).FieldByName("a")
		got, gotFound, _, err := FindQualifiedField[lvl1]("a", lvl1{})
		if err != nil {
			panic(err)
		}
		if gotFound != expFound {
			t.Errorf("found mismatch: got(%v) != expected(%v)", gotFound, expFound)
		}

		if !reflect.DeepEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
			return
		}
	})
	t.Run("depth 0 pointer", func(t *testing.T) {
		exp, expFound := reflect.TypeOf(lvl2{}).FieldByName("c")
		got, gotFound, _, err := FindQualifiedField[lvl2]("c", lvl2{})
		if err != nil {
			panic(err)
		}
		if gotFound != expFound {
			t.Errorf("found mismatch: got(%v) != expected(%v)", gotFound, expFound)
		}

		if !reflect.DeepEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
			return
		}
	})

	t.Run("promoted", func(t *testing.T) {
		exp, expFound := reflect.TypeOf(lvl1{}).FieldByName("b")
		got, gotFound, _, err := FindQualifiedField[lvl1]("b", lvl1{})
		if err != nil {
			panic(err)
		}
		if gotFound != expFound {
			t.Errorf("found mismatch: got(%v) != expected(%v)", gotFound, expFound)
		}

		if !reflect.DeepEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
			return
		}
	})

	t.Run("promoted pointer", func(t *testing.T) {
		exp, expFound := reflect.TypeOf(lvl1{}).FieldByName("c")
		got, gotFound, _, err := FindQualifiedField[lvl1]("c", lvl1{})
		if err != nil {
			panic(err)
		}
		if gotFound != expFound {
			t.Errorf("found mismatch: got(%v) != expected(%v)", gotFound, expFound)
		}

		if !reflect.DeepEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}
	})
	t.Run("named struct navigation", func(t *testing.T) {

		var expIndexPath []int = []int{1, 0}
		var exp reflect.StructField = reflect.TypeOf(lvl1{}).FieldByIndex(expIndexPath)

		got, _, gotIndexPath, err := FindQualifiedField[lvl1]("l2.b", lvl1{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}

		if len(gotIndexPath) != len(expIndexPath) {
			t.Errorf("path len mismatch: gotPath(%v) != expectedPath(%v)", gotIndexPath, expIndexPath)
		}

		for i := 0; i < len(gotIndexPath); i++ {
			if gotIndexPath[i] != expIndexPath[i] {
				t.Errorf("path mismatch @ index [%d]: gotPath(%v) != expectedPath(%v)", i, gotIndexPath, expIndexPath)
			}
		}

	})
	t.Run("named struct navigation outer -> (embed) -> too -> mu", func(t *testing.T) {
		var expIndexPath []int = []int{0, 1, 0}
		var exp reflect.StructField = reflect.TypeOf(outer{}).FieldByIndex(expIndexPath)

		got, _, gotIndexPath, err := FindQualifiedField[outer]("too.mu", outer{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}

		if len(gotIndexPath) != len(expIndexPath) {
			t.Errorf("path len mismatch: gotPath(%v) != expectedPath(%v)", gotIndexPath, expIndexPath)
		}

		for i := 0; i < len(gotIndexPath); i++ {
			if gotIndexPath[i] != expIndexPath[i] {
				t.Errorf("path mismatch @ index [%d]: gotPath(%v) != expectedPath(%v)", i, gotIndexPath, expIndexPath)
			}
		}

	})
	t.Run("named struct navigation outer -> (embed) -> too -> mu fail (no equity)", func(t *testing.T) {

		// access one field too far within too

		var exp reflect.StructField = reflect.TypeOf(outer{}).FieldByIndex([]int{0, 1, 1})

		got, _, _, err := FindQualifiedField[lvl1]("too.mu", outer{})
		if err != nil {
			panic(err)
		}

		if structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch expected but found equity: got(%v) != expected(%v)", got, exp)
			return
		}
	})
	t.Run("named struct navigation ptr", func(t *testing.T) {

		var exp reflect.StructField = reflect.TypeOf(lvl1{}).FieldByIndex([]int{0, 1})

		got, _, _, err := FindQualifiedField[lvl1]("l2.c", lvl1{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
			return
		}
	})

	t.Run("embedded + depth 2", func(t *testing.T) {
		var exp reflect.StructField = reflect.TypeOf(lvl1{}).FieldByIndex([]int{0, 2, 0})

		got, _, _, err := FindQualifiedField[lvl1]("l3.d", lvl1{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
			return
		}
	})

	t.Run("depth 3", func(t *testing.T) {
		var exp reflect.StructField = reflect.TypeOf(lvl1{}).FieldByIndex([]int{0, 2, 0})

		got, _, _, err := FindQualifiedField[lvl1]("l2.l3.d", lvl1{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}
	})

	// test accessing fields within first-class struct, e, embedded at depth 0,
	// in struct lvl3
	t.Run("first-class internal struct @ depth 0", func(t *testing.T) {
		var exp reflect.StructField = reflect.TypeOf(lvl3{}).FieldByIndex([]int{1, 1})

		got, _, _, err := FindQualifiedField[lvl3]("e.b", lvl3{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}
	})

	t.Run("deeply nested first-class struct", func(t *testing.T) {
		var exp reflect.StructField = reflect.TypeOf(lvl1{}).FieldByIndex([]int{1, 2, 1, 1})

		got, _, _, err := FindQualifiedField[lvl1]("l2.l3.e.b", lvl1{})
		if err != nil {
			panic(err)
		}

		if !structFieldsEqual(got, exp) {
			t.Errorf("equality mismatch: got(%v) != expected(%v)", got, exp)
		}
	})

	t.Run("empty column name", func(t *testing.T) {
		field, found, index, err := FindQualifiedField[lvl1]("", lvl1{})
		if err != nil {
			t.Error(err)
		}
		if found != false {
			t.Error("found field. expected found == false")
		}
		if index != nil {
			t.Errorf("index should be nil. Got: %v", index)
		}
		if !reflect.DeepEqual(field, reflect.StructField{}) {
			t.Errorf("field mismatch. Expected empty StructField{}, got: %v", field)
		}
	})

	t.Run("nil struct", func(t *testing.T) {
		field, found, index, err := FindQualifiedField[lvl1]("a", nil)
		want := errors.New(ErrStructIsNil)
		if errors.Is(err, want) {
			t.Errorf("Expected '%v', got '%v'", want, err)
		}
		if found != false {
			t.Error("found field. expected found == false")
		}
		if index != nil {
			t.Errorf("index should be nil. Got: %v", index)
		}
		if !reflect.DeepEqual(field, reflect.StructField{}) {
			t.Errorf("field mismatch. Expected empty StructField{}, got: %v", field)
		}
	})

	t.Run("not a struct", func(t *testing.T) {
		field, found, index, err := FindQualifiedField[map[string]string]("a", map[string]string{})
		want := errors.New(ErrNotAStruct)
		if errors.Is(err, want) {
			t.Errorf("Expected '%v', got '%v'", want, err)
		}
		if found != false {
			t.Error("found field. expected found == false")
		}
		if index != nil {
			t.Errorf("index should be nil. Got: %v", index)
		}
		if !reflect.DeepEqual(field, reflect.StructField{}) {
			t.Errorf("field mismatch. Expected empty StructField{}, got: %v", field)
		}
	})
}

// Fields returned by FindQualifiedField retain their true, nested index while
// fetching via FindByIndex or iterative Field() calls do not.
// Therefore, we cannot use DeepEqual() for comparison, but want to compare as
// much else as possible and makes sense for all primatives.
func structFieldsEqual(x reflect.StructField, y reflect.StructField) bool {
	return (x.Anonymous == y.Anonymous &&
		x.Name == y.Name &&
		x.Offset == y.Offset &&
		x.PkgPath == y.PkgPath &&
		x.Tag == y.Tag &&
		x.Type == y.Type &&
		x.IsExported() == y.IsExported() &&
		x.Type.Align() == y.Type.Align())
}

func TestStructFieldsErrors(t *testing.T) {
	t.Run("struct is nil", func(t *testing.T) {
		c, err := StructFields(nil, true)
		if err.Error() != ErrStructIsNil || c != nil {
			t.Errorf("Error value mismatch: err: %v c: %v", err, c)
		}
	})
	t.Run("not a struct", func(t *testing.T) {
		m := make(map[string]int)
		c, err := StructFields(m, true)
		if err.Error() != ErrNotAStruct || c != nil {
			t.Errorf("Error value mismatch: err: %v c: %v", err, c)
		}
	})
}

type dblmbd struct {
	Y string
}
type mbd struct {
	dblmbd
	z string
}
type triple struct {
	mbd
	ins mbd
	dbl dblmbd
	A   int
	b   uint
}

type inner2 struct {
	z    *string
	None string
}

type ptr struct {
	A        *int
	b        *int
	innerptr *inner2
	Inner    inner2
	non      string
}

func TestStructFieldsAll(t *testing.T) {

	// silence "unused" warnings as we only care about types
	a, b, z, non := 1, 2, "Z", "NON"
	var _ ptr = ptr{A: &a, b: &b, innerptr: &inner2{z: &z, None: "NONE"}, Inner: inner2{}, non: non}

	type args struct {
		st any
	}

	triple_want := []string{"mbd.dblmbd.Y", "mbd.z", "ins.dblmbd.Y", "ins.z", "dbl.Y", "A", "b"}

	tests := []struct {
		name        string
		args        args
		wantColumns []string
	}{
		{"single level", args{st: dblmbd{Y: "y string"}}, []string{"Y"}},
		{"second level", args{st: mbd{z: "z string", dblmbd: dblmbd{Y: "y sting"}}}, []string{"dblmbd.Y", "z"}},
		{"third level", args{
			st: triple{
				A:   -780,
				b:   1,
				dbl: dblmbd{Y: "y string"},
				ins: mbd{z: "z string", dblmbd: dblmbd{Y: "y string 2"}},
				mbd: mbd{dblmbd: dblmbd{Y: "y string 3"},
					z: "z string 2"},
			}}, triple_want},
		{"third level valueless", args{st: triple{}}, triple_want},
		{"third level pointer", args{st: &triple{}}, triple_want},
		{"pointers", args{ptr{}}, []string{"A", "b", "innerptr.z", "innerptr.None", "Inner.z", "Inner.None", "non"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotColumns, err := StructFields(tt.args.st, false)
			if err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(gotColumns, tt.wantColumns) {
				t.Errorf("StructFields() = %v, want %v", gotColumns, tt.wantColumns)
			}
		})
	}
}

func TestStructFieldsExported(t *testing.T) {

	triple_want := []string{"A"}

	tests := []struct {
		name        string
		args        any
		wantColumns []string
	}{
		{"single level", dblmbd{Y: "y string"}, []string{"Y"}},
		{"second level", mbd{z: "z string", dblmbd: dblmbd{Y: "y sting"}}, []string{}},
		{"third level",
			triple{
				A:   -780,
				b:   1,
				dbl: dblmbd{Y: "y string"},
				ins: mbd{z: "z string", dblmbd: dblmbd{Y: "y string 2"}},
				mbd: mbd{dblmbd: dblmbd{Y: "y string 3"},
					z: "z string 2"},
			}, triple_want},
		{"third level valueless", triple{}, triple_want},
		{"third level pointer", &triple{}, triple_want},
		{"pointers", ptr{}, []string{"A", "Inner.None"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotColumns, err := StructFields(tt.args, true)
			if err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(gotColumns, tt.wantColumns) {
				t.Errorf("StructFields() = %v, want %v", gotColumns, tt.wantColumns)
			}
		})
	}
	//#endregion exportedOnly
}
