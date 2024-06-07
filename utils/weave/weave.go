/**
 * The output module.
 * Weave consumes arbitrary structs, orchestrating them into a specified format
 * and returning the formatted string.
 */
package weave

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Jeffail/gabs/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

//#region errors

const (
	ErrNotAStruct  string = "given value is not a struct or pointer to a struct"
	ErrStructIsNil string = "given value is nil"
)

//#endregion

// Takes an array of arbitrary struct `st` and the *ordered* columns to
// include/exclude and returns a string containing the csv representation of the
// data contained therein.
//
// ! Returns the empty string if columns or st are empty
func ToCSV[Any any](st []Any, columns []string) string {
	// DESIGN:
	// We have a list of column, ordered.
	// We have a map of column names -> field index.
	// For each struct s in the list of structs:
	//	iterate through the list of columns and use the map to fetch the
	//	column/field's values by index, building the csv token by token

	if columns == nil || st == nil || len(st) < 1 || len(columns) < 1 { // superfluous request
		return ""
	}

	// test the first struct is actually a struct
	// if later structs do not match, that is a developer error
	if reflect.TypeOf(st[0]).Kind() != reflect.Struct {
		return ""
	}

	columnMap := buildColumnMap(st[0], columns)

	var hdr string = strings.Join(columns, ",")

	var csv strings.Builder // stores the actual data

	for _, s := range st { // operate on each struct'
		csv.WriteString(stringifyStructCSV(s, columns, columnMap) + "\n")
	}

	return strings.TrimSpace(hdr + "\n" + csv.String())
}

// helper function for ToCSVHash
// returns a string of a CSV row populated by the data in the struct that corresponds to the columns
func stringifyStructCSV(s interface{}, columns []string, columnMap map[string][]int) string {
	var row strings.Builder

	// deconstruct the struct
	structVals := reflect.ValueOf(s)

	// search for each column
	for _, col := range columns {
		findices := columnMap[col]
		if findices == nil {
			// no matching field
			// do nothing
		} else {
			// use field index to retrieve value
			data := structVals.FieldByIndex(findices)
			if data.Kind() == reflect.Pointer {
				data = data.Elem()
			}
			row.WriteString(fmt.Sprintf("%v", data))
		}
		row.WriteString(",") // append comma to token
	}

	return strings.TrimSuffix(row.String(), ",")
}

// Given an array of an arbitrary struct and the list of *fully-qualified* fields,
// outputs a table containing the data in the array of the struct.
//
// Can optionally be given a table style func. Uses DefaultTblStyle() if not given.
func ToTable[Any any](st []Any, columns []string, styleFunc ...func() *table.Table) string {
	if columns == nil || st == nil || len(st) < 1 || len(columns) < 1 { // superfluous request
		return ""
	}

	columnMap := buildColumnMap(st[0], columns)

	var rows [][]string = make([][]string, len(st))

	for i := range st { // operate on each struct
		rows[i] = make([]string, len(columns))
		// deconstruct the struct
		structVals := reflect.ValueOf(st[i])
		// search for each column
		for k := range columns {
			findex := columnMap[columns[k]]
			if findex != nil {
				data := structVals.FieldByIndex(findex)
				if data.Kind() == reflect.Pointer {
					data = data.Elem()
				}
				// save the data into our row
				rows[i][k] = fmt.Sprintf("%v", data)
			}
		}
	}

	var tbl *table.Table
	// if user supplied a tableStyle, use it. Otherwise, use the default
	if len(styleFunc) > 0 {
		tbl = styleFunc[0]()
	} else {
		tbl = DefaultTblStyle()
	}

	tbl.Headers(columns...)
	tbl.Rows(rows...)

	return tbl.Render()
}

// Style function used internally by ToTable if a styleFunc is not provided.
// Use as an example for supplying your own.
func DefaultTblStyle() *table.Table {
	return table.New().StyleFunc(func(row, col int) lipgloss.Style {
		return lipgloss.NewStyle().Width(10) // set set row and column width
	})
}

// transmogrification struct for outputting complex numbers that encoding/json
// otherwise doesn't support
type gComplex[t float32 | float64] struct {
	Real      t
	Imaginary t
}

// Given an array of an arbitrary struct and the list of *fully-qualified* fields,
// outputs a JSON array containing the data in the array of the struct.
// Output is sorted alphabetically
func ToJSON[Any any](st []Any, columns []string) (string, error) {
	if columns == nil || st == nil || len(st) < 1 || len(columns) < 1 { // superfluous request
		return "[]", nil
	}

	columnMap := buildColumnMap(st[0], columns)

	var bldr strings.Builder
	bldr.WriteRune('[') // open JSON array
	for _, s := range st {
		g := gabs.New()
		structVO := reflect.ValueOf(s)
		for _, col := range columns {
			// get value associated to this column
			fIndex := columnMap[col]
			if fIndex != nil {
				data := structVO.FieldByIndex(fIndex)
				if data.Kind() == reflect.Pointer {
					data = data.Elem()
				}
				switch data.Type().Kind() {
				case reflect.Float32:
					v := data.Interface().(float32)
					g.SetP(v, col)
				case reflect.Float64:
					v := data.Interface().(float64)
					g.SetP(v, col)
				case reflect.Int:
					v := data.Interface().(int)
					g.SetP(v, col)
				case reflect.Int8:
					v := data.Interface().(int8)
					g.SetP(v, col)
				case reflect.Int16:
					v := data.Interface().(int16)
					g.SetP(v, col)
				case reflect.Int32:
					v := data.Interface().(int32)
					g.SetP(v, col)
				case reflect.Int64:
					v := data.Interface().(int64)
					g.SetP(v, col)
				case reflect.Complex64:
					v := data.Interface().(complex64)
					gC := gComplex[float32]{Real: real(v), Imaginary: imag(v)}
					if _, err := g.SetP(gC, col); err != nil {
						return "", err
					}
				case reflect.Complex128:
					v := data.Interface().(complex128)
					gC := gComplex[float64]{Real: real(v), Imaginary: imag(v)}
					if _, err := g.SetP(gC, col); err != nil {
						return "", err
					}
				case reflect.Array, reflect.Slice:
					// arrays must be iterated through and rebuilt to retain
					// proper typing
					g.ArrayP(col)
					// append each item in the array
					iCount := data.Len()
					for i := 0; i < iCount; i++ {
						g.ArrayAppendP(data.Index(i).Interface(), col)
					}
				case reflect.Uint:
					v := data.Interface().(uint)
					g.SetP(v, col)
				case reflect.Uint8:
					v := data.Interface().(uint8)
					g.SetP(v, col)
				case reflect.Uint16:
					v := data.Interface().(uint16)
					g.SetP(v, col)
				case reflect.Uint32:
					v := data.Interface().(uint32)
					g.SetP(v, col)
				case reflect.Uint64:
					v := data.Interface().(uint64)
					g.SetP(v, col)
				case reflect.String:
					v := data.Interface().(string)
					g.SetP(v, col)
				default: // unsupported type, default to string
					g.SetP(fmt.Sprintf("%v", data), col)
				}
			}
		}
		bldr.WriteString(g.String())
		bldr.WriteRune(',') // new entry
	}
	toRet := strings.TrimSuffix(bldr.String(), ",") // chomp final comma

	return toRet + "]", nil // close JSON array
}

// BROKEN UNTIL Gabs ISSUE#141 IS RESOLVED
// Given an array of an arbitrary struct, outputs a JSON array containing the
// data in the array of the struct, minus the blacklisted columns
// Output is sorted alphabetically
func ToJSONExclude[Any any](st []Any, blacklist []string) (string, error) {
	if st == nil || len(st) < 1 { // superfluous request
		return "[]", errors.New(ErrStructIsNil)
	}

	// test the first struct is actually a struct
	// if later structs do not match, that is a developer error
	if reflect.TypeOf(st[0]).Kind() != reflect.Struct {
		return "[]", errors.New(ErrNotAStruct)
	}

	var writer strings.Builder
	writer.WriteRune('[')
	for _, s := range st {
		obj := gabs.Wrap(s)
		// remove excluded colums
		/*for _, col := range blacklist {
			if err := obj.DeleteP(col); err != nil {
				return "", fmt.Errorf("column %s: %v", col, err)
			}
		}*/
		// add to array
		writer.WriteString(obj.String())
		writer.WriteString(",")
	}

	// chip, close, and return
	return strings.TrimSuffix(writer.String(), ",") + "]", nil
}

// Given a fully qualified column name (ex: "outerstruct.innerstruct.field"),
// finds the associated field, if it exists.
//
// Qualifications follow Go's rules for nested structs, including embedded
// variable promotion.
//
// Returns the field, whether or not it was found, the index path (for
// FieldByIndex) to the field (more on this below), and any errors.
//
// ! st must be a struct
func FindQualifiedField[Any any](qualCol string, st any) (field reflect.StructField, found bool, index []int, err error) {
	// Design Note:
	// Index path is returned becaue field.Index is NOT reliable for some
	// nested fields. Fields do not necessarily know their complete index path
	// for the given parent struct and therefore using field.Index in FieldByIndex
	// can cause unexpected, erroneous reults (generally fetching items at a
	// higher depth than the field actually is).
	// The returned index path is composed of the known indices of every field
	// touched during traversal, returning a complete path.

	// pre checks
	if qualCol == "" {
		return reflect.StructField{}, false, nil, nil
	}
	if st == nil {
		return reflect.StructField{}, false, nil, errors.New(ErrStructIsNil)
	}
	t := reflect.TypeOf(st)
	if t.Kind() != reflect.Struct {
		return reflect.StructField{}, false, nil, errors.New(ErrNotAStruct)
	}

	index = make([]int, 0)

	exploded := strings.Split(qualCol, ".")
	field.Type = t
	// iterate down the field tree until we run out of qualifications or cannot
	// locate the next qualification
	for _, e := range exploded {
		if field.Type.Kind() == reflect.Pointer {
			field.Type = field.Type.Elem() // dereference
		}
		field, found = field.Type.FieldByName(e)
		if !found { // no value found
			//fmt.Printf("Found no value for qualifier '%s' at depth %d\n", e, i)
			return reflect.StructField{}, false, nil, nil
		}
		// build path
		index = append(index, field.Index...)
	}
	// if we reached the end of the loop, we have our final field
	return field, true, index, nil

}

// Returns the fully qualified name of every (exported) field in the struct
// *definition*, as they are ordered internally
// These qualified names are the expected format for the output modules in this
// package
func StructFields(st any, exportedOnly bool) (columns []string, err error) {
	if st == nil {
		return nil, errors.New(ErrStructIsNil)
	}
	to := reflect.TypeOf(st)
	if to.Kind() == reflect.Pointer { // dereference
		to = to.Elem()
	}
	if to.Kind() != reflect.Struct { // prerequisite
		return nil, errors.New(ErrNotAStruct)
	}
	numFields := to.NumField()
	columns = []string{}

	// for each field
	//	if the field is not a struct, append it to the columns
	//	if the field is a struct, repeat

	for i := 0; i < numFields; i++ {
		columns = append(columns, innerStructFields("", to.Field(i), exportedOnly)...)
	}

	return columns, nil
}

// innerStructFields is a helper function for StructFields, returning the
// qualified name of the given field or the list of qualified names of its
// children, if a struct.
// Operates recursively on the given field if it is a struct.
// Operates down the struct, in field-order.
func innerStructFields(qualification string, field reflect.StructField, exportedOnly bool) []string {
	var columns []string = []string{}

	// do not operate on unexported fields if exportedOnly
	if exportedOnly && !field.IsExported() {
		return columns
	}

	// dereference
	if field.Type.Kind() == reflect.Ptr {
		field.Type = field.Type.Elem()
	}

	if field.Type.Kind() == reflect.Struct {
		for k := 0; k < field.Type.NumField(); k++ {
			var innerQual string
			if qualification == "" {
				innerQual = field.Name
			} else {
				innerQual = qualification + "." + field.Name
			}
			columns = append(columns, innerStructFields(innerQual, field.Type.Field(k), exportedOnly)...)
		}
	} else {
		if qualification == "" {
			columns = append(columns, field.Name)
		} else {
			columns = append(columns, qualification+"."+field.Name)
		}
	}

	return columns
}

// Given a struct and the desired fields (columns), maps the full, qualified
// field names to their complete index chain. If a field is not found in the
// struct, its value is set to nil in the map.
func buildColumnMap(st any, columns []string) (columnMap map[string][]int) {
	numColumns := len(columns)

	// deconstruct the first struct to validate requested columns
	// coordinate columns
	columnMap = make(map[string][]int, numColumns) // column name -> recursive field indices
	for i := range columns {
		// map column names to their field indices
		// if a name is not found, nil it so it can be skipped later
		_, fo, index, err := FindQualifiedField[any](columns[i], st)
		if err != nil {
			panic(err)
		}
		if !fo {
			columnMap[columns[i]] = nil
			continue
		}
		columnMap[columns[i]] = index
	}
	return
}
