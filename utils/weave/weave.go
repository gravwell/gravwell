/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package weave consumes arbitrary structs, orchestrating them into a specified format and returning the formatted string.
package weave

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Jeffail/gabs/v2"
	"github.com/charmbracelet/lipgloss/table"
)

//#region errors

const (
	ErrNotAStruct  string = "given value is not a struct or pointer to a struct"
	ErrStructIsNil string = "given value is nil"
)

func errFailedKindAssert(assertType string, kind string) error {
	return fmt.Errorf("cannot assert to %s despite %s kind", assertType, kind)
}

//#endregion

// ToCSV takes an array of arbitrary struct `st` and the *ordered* columns to
// include/exclude and returns a string containing the csv representation of the
// data contained therein.
//
// ! Returns the empty string if columns or st are empty
func ToCSV[Any any](st []Any, columns []string, options CSVOptions) string {
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

	// generate header line, referencing aliases if relevant
	var hdr string
	if options.Aliases != nil {
		var sb strings.Builder
		for _, col := range columns {
			if alias, found := options.Aliases[col]; found {
				sb.WriteString(alias + ",")
			} else {
				sb.WriteString(col + ",")
			}
		}
		hdr = sb.String()[:sb.Len()-1]
	} else {
		hdr = strings.Join(columns, ",")
	}

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

// ToTable when given an array of an arbitrary struct and the list of *fully-qualified* fields,
// outputs a table containing the data in the array of the struct.
// If no columns are specified or st is nil, returns the empty string.
// If ToTable encounters a nil pointer while traversing the data, it will populate the cell (and the cells of all child fields) with "nil".
func ToTable[Any any](st []Any, columns []string, options TableOptions) string {
	if len(st) < 1 || len(columns) < 1 { // superfluous request
		return ""
	}

	columnMap := buildColumnMap(st[0], columns)

	var rows = make([][]string, len(st))

	for i := range st { // operate on each struct
		rows[i] = make([]string, len(columns))
		// deconstruct the struct
		structVals := reflect.ValueOf(st[i])
		// search for each column
		for k := range columns {
			findicies := columnMap[columns[k]]
			if findicies != nil {
				// manually step through the struct to check for nils.
				// NOTE(rlandau): we use this instead of just passing the slice to FieldByIndex because FbI panics on attempting to step through a nil pointer.
				// Panicking won't work for us.
				var (
					invalid bool
					data    = structVals
				)
				for _, findex := range findicies {
					// step one level lower
					data = data.Field(findex)
					if data.Kind() == reflect.Ptr {
						if data.IsNil() { // stop traveling at a nil pointer
							invalid = true
							break
						}
						data = data.Elem() // otherwise, dereference to continue to delve
					}
					// data.Kind() != reflect.Struct
				}

				// if we failed to walk to the end of the indices,
				if invalid {
					rows[i][k] = "nil"
					continue
				}

				if data.Kind() == reflect.Pointer {
					data = data.Elem()
				}
				// save the data into our row
				rows[i][k] = fmt.Sprintf("%v", data)
			}
		}
	}

	// generate the table
	var tbl *table.Table
	if options.Base != nil {
		tbl = options.Base()
	} else {
		tbl = table.New()
	}

	// apply aliases
	if options.Aliases != nil {
		withAliases := make([]string, len(columns))
		for i := range columns {
			// on match, replace the column
			if alias, found := options.Aliases[columns[i]]; found {
				withAliases[i] = alias
			} else {
				withAliases[i] = columns[i]
			}
		}
		tbl.Headers(withAliases...)
	} else {
		tbl.Headers(columns...)
	}
	tbl.Rows(rows...)

	return tbl.Render()
}

// transmogrification struct for outputting complex numbers that encoding/json
// otherwise doesn't support
type gComplex[t float32 | float64] struct {
	Real      t
	Imaginary t
}

// ToJSON when given an array of an arbitrary struct and the list of *fully-qualified* fields,
// outputs a JSON array containing the data in the array of the struct.
// Output is sorted alphabetically
func ToJSON[Any any](st []Any, columns []string, options JSONOptions) (string, error) {
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
				// if there is an alias, we write that as the key instead
				if alias, found := options.Aliases[col]; found {
					col = alias
				}

				switch data.Type().Kind() {
				case reflect.Float32:
					if !data.CanFloat() {
						return "", errFailedKindAssert("float", "Float32")
					}
					if _, err := g.SetP(float32(data.Float()), col); err != nil {
						return "", err
					}
				case reflect.Float64:
					if !data.CanFloat() {
						return "", errFailedKindAssert("float", "Float64")
					}
					if _, err := g.SetP(float64(data.Float()), col); err != nil {
						return "", err
					}
				case reflect.Int:
					if !data.CanInt() {
						return "", errFailedKindAssert("int", "Int")
					}
					if _, err := g.SetP(int(data.Int()), col); err != nil {
						return "", err
					}
				case reflect.Int8:
					if !data.CanInt() {
						return "", errFailedKindAssert("int", "Int8")
					}
					if _, err := g.SetP(int8(data.Int()), col); err != nil {
						return "", err
					}
				case reflect.Int16:
					if !data.CanInt() {
						return "", errFailedKindAssert("int", "Int16")
					}
					if _, err := g.SetP(int16(data.Int()), col); err != nil {
						return "", err
					}
				case reflect.Int32:
					if !data.CanInt() {
						return "", errFailedKindAssert("int", "Int32")
					}
					if _, err := g.SetP(int32(data.Int()), col); err != nil {
						return "", err
					}
				case reflect.Int64:
					if !data.CanInt() {
						return "", errFailedKindAssert("int", "Int64")
					}
					g.SetP(data.Int(), col)
				case reflect.Complex64:
					if !data.CanComplex() {
						return "", errFailedKindAssert("complex", "Complex64")
					}
					v := complex64(data.Complex())
					gC := gComplex[float32]{Real: real(v), Imaginary: imag(v)}
					if _, err := g.SetP(gC, col); err != nil {
						return "", err
					}
				case reflect.Complex128:
					if !data.CanComplex() {
						return "", errFailedKindAssert("complex", "Complex128")
					}
					v := data.Complex()
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
					for i := range iCount {
						g.ArrayAppendP(data.Index(i).Interface(), col)
					}
				case reflect.Uint:
					if !data.CanUint() {
						return "", errFailedKindAssert("uint", "Uint")
					}
					if _, err := g.SetP(uint(data.Uint()), col); err != nil {
						return "", err
					}
				case reflect.Uint8:
					if !data.CanUint() {
						return "", errFailedKindAssert("uint", "Uint8")
					}
					if _, err := g.SetP(uint8(data.Uint()), col); err != nil {
						return "", err
					}
				case reflect.Uint16:
					if !data.CanUint() {
						return "", errFailedKindAssert("uint", "Uint16")
					}
					if _, err := g.SetP(uint16(data.Uint()), col); err != nil {
						return "", err
					}
				case reflect.Uint32:
					if !data.CanUint() {
						return "", errFailedKindAssert("uint", "Uint32")
					}
					if _, err := g.SetP(uint32(data.Uint()), col); err != nil {
						return "", err
					}
				case reflect.Uint64:
					if !data.CanUint() {
						return "", errFailedKindAssert("uint", "Uint64")
					}
					if _, err := g.SetP(data.Uint(), col); err != nil {
						return "", err
					}
				case reflect.String:
					v := data.String()
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

// ToJSONExclude is BROKEN UNTIL Gabs ISSUE#141 IS RESOLVED
// Given an array of an arbitrary struct, outputs a JSON array containing the
// data in the array of the struct, minus the blacklisted columns
// Output is sorted alphabetically
func ToJSONExclude[Any any](st []Any, blacklist []string) (string, error) {
	if len(st) < 1 { // superfluous request
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

// FindQualifiedField when given a fully qualified column name (ex: "outerstruct.innerstruct.field"),
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

// StructFields returns the fully qualified name of every (exported) field in the struct
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
	var columns = []string{}

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
	// deconstruct the first struct to validate requested columns
	// coordinate columns
	columnMap = make(map[string][]int, len(columns)) // column name -> recursive field indices
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
