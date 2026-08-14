# Weave

Weave provides the ability to turn data stored in arbitrary structs of arbitrary depth into exportable formats like CSV or JSON.
It supports field selection, named/unnamed structs, and embeds.

# Usage

Basic usage is via the output modules (`To*`). Simply pass your array of the *same struct* to an output module along with the fully qualified (more on this below) names of the columns you want outputted.

Ex: `out := ToCSV(data, []string{"fieldname", "structname.anotherinnerstruct.fieldname"})`

Call `StructField()` on your struct to see the full, qualified names of every field at every depth.

## Example

```go
type someEmbed struc {
	Fld int
}

type someData struct {
	someEmbed
	A int
}
data := someData{someEmbed: someEmbed{Fld: 5}, A: 10}

output := ToCSV(data, []string{"A"})

fmt.Println(output)
```

See the test suite in [weave_test](weave_test.go) for more usage examples.

## Dot Qualification

Column names are dot qualified and follow Go's rules for struct nesting and promotion. They are all compatible with [Gabs](https://pkg.go.dev/github.com/Jeffail/gabs/v2) paths.

To repeat: call `StructField()` on your struct to see the full, qualified names of every field at every depth.

### Examples

#### Basic

```go
type A struct {
	a int
	b int
	C int
}
```

Can be accessed directly ("a", "b", "C").

#### Embedding

```go
type mbd struct {
	X string
	z string
}

type A struct {
	a int
	b int
	C int
	mbd
}
```

Embedded field are accessed as "X" and "z".

#### Structs Within Structs

```go
type deep struct {
	F float 64
}

type shallow struct {
	D deep
	X string
	z string
}

type A struct {
	a int
	b int
	C int
	i inner
}
```

"i.D.F", "i.z"

# Limitations

- Column names (and qualifications) are case sensitive

## ToJSON

- Encoding/json does not accept complex numbers. Weave can by converting them to a generic struct and outputing the struct as JSON objects with the fields "Real" and "Imaginary".

- As with normal JSON encoding, only exported struct fields can be output.

## ToJSONExclude

The blacklist parameter is currently ineffectual due to a bug in how the JSON library (Gabs) `.Wrap()`s existing structures. I have opened a PR [#142](https://github.com/Jeffail/gabs/pull/142) to fix this.

This function will instead output ALL exported fields at all depths.

# TODOs

- [ ] Create ToCSVExclude, consuming column list as blacklist
- [ ] Make qualified column names case-insensitive