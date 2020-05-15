/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/gravwell/gcfg"
)

const (
	maxConfigSize int64 = 4 * mb // This is a MASSIVE config file
)

var (
	ErrConfigFileTooLarge     = errors.New("Config file is too large")
	ErrFailedFileRead         = errors.New("Failed to read entire config file")
	ErrConfigNotOpen          = errors.New("Configuration is not open")
	ErrInvalidImportInterface = errors.New("Invalid import interface argument")
	ErrInvalidImportParameter = errors.New("parameter is not a pointer")
	ErrInvalidArgument        = errors.New("Invalid argument")
	ErrInvalidMapKeyType      = errors.New("invalid map key type, must be string")
	ErrInvalidMapValueType    = errors.New("invalid map value type, must be pointer to struct")
	ErrBadMap                 = errors.New("VariableConfig has not be initialized")
)

type VariableConfig struct {
	gcfg.Idxer
	Vals map[gcfg.Idx]*[]string
}

// LoadConfigFile will open a config file, check the file size
// and load the bytes using LoadConfigBytes
func LoadConfigFile(v interface{}, p string) (err error) {
	var fin *os.File
	var fi os.FileInfo
	var n int64
	if fin, err = os.Open(p); err != nil {
		return
	} else if fi, err = fin.Stat(); err != nil {
		fin.Close()
		return
	} else if fi.Size() > maxConfigSize {
		fin.Close()
		err = ErrConfigFileTooLarge
		return
	}

	bb := bytes.NewBuffer(nil)
	if n, err = io.Copy(bb, fin); err != nil {
		fin.Close()
		return
	} else if n != fi.Size() {
		fin.Close()
		err = ErrFailedFileRead
	} else if err = fin.Close(); err == nil {
		err = LoadConfigBytes(v, bb.Bytes())
	}
	return
}

// LoadConfigBytes parses the contents of b into the given interface v.
func LoadConfigBytes(v interface{}, b []byte) error {
	if int64(len(b)) > maxConfigSize {
		return ErrConfigFileTooLarge
	}
	return gcfg.ReadStringInto(v, string(b))
}

// importMaps walks the structure using reflection and imports any members that are map types
// if the map is nil we initialize it
// the the goINI package doesn't handle map types for sub sections so we have to do it ourselves
/*
func (c *Config) importMaps(v interface{}) error {
	if reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidImportParameter
	}
	vv := reflect.ValueOf(v).Elem()
	tot := vv.Type()
	for i := 0; i < vv.NumField(); i++ {
		fld := vv.Field(i)
		if fld.Kind() == reflect.Map {
			// and the value is a pointer to a struct
			if err := c.importMap(tot.Field(i).Name, fld); err != nil {
				return err
			}
		}
	}
	return nil
}
*/

func (vc VariableConfig) MapTo(v interface{}) (err error) {
	if vc.Vals == nil {
		err = ErrBadMap
	} else if v == nil {
		err = ErrInvalidImportParameter
	} else if reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidImportParameter
	} else {
		err = vc.mapStruct(v)
	}
	return
}

func (vc VariableConfig) get(name string) (v string, ok bool) {
	var temp *[]string
	if temp = vc.Vals[vc.Idx(name)]; temp != nil {
		var x []string
		x = *temp
		if len(x) > 0 {
			v = x[0]
			ok = true
		}
	}
	return
}

func (vc VariableConfig) getSlice(name string) (v []string, ok bool) {
	var temp *[]string
	if temp = vc.Vals[vc.Idx(name)]; temp != nil {
		v = *temp
		ok = true
	}
	return
}

func (vc VariableConfig) mapStruct(v interface{}) error {
	if reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidImportParameter
	}
	// ensure the value is a pointer to a struct
	rv := reflect.ValueOf(v).Elem()
	if rv.Type().Kind() != reflect.Struct {
		return ErrInvalidMapValueType
	}
	typeOf := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		if err := vc.setField(typeOf.Field(i).Name, rv.Field(i)); err != nil {
			return err
		}
	}
	return nil
}

// TODO FIXME - figure out how to deal with slices of a type so that we add to them
func (vc VariableConfig) setField(name string, v reflect.Value) (err error) {
	strv, ok := vc.get(nameMapper(name))
	if !ok {
		return
	}
	switch v.Type().Kind() {
	case reflect.Int8:
		fallthrough
	case reflect.Int16:
		fallthrough
	case reflect.Int32:
		fallthrough
	case reflect.Int64:
		fallthrough
	case reflect.Int:
		var vint int64
		if vint, err = ParseInt64(strv); err == nil {
			if v.OverflowInt(vint) {
				err = fmt.Errorf("%d overflows %T", vint, v.Interface())
			} else {
				v.SetInt(vint)
			}
		}
	case reflect.Uint8:
		fallthrough
	case reflect.Uint16:
		fallthrough
	case reflect.Uint32:
		fallthrough
	case reflect.Uint64:
		fallthrough
	case reflect.Uint:
		var vint uint64
		if vint, err = ParseUint64(strv); err == nil {
			if v.OverflowUint(vint) {
				err = fmt.Errorf("%d overflows %T", vint, v.Interface())
			} else {
				v.SetUint(vint)
			}
		}
	case reflect.Float32:
		fallthrough
	case reflect.Float64:
		var vf float64
		if vf, err = strconv.ParseFloat(strv, 64); err == nil {
			if v.OverflowFloat(vf) {
				err = fmt.Errorf("%f overflows %T", vf, v.Interface())
			} else {
				v.SetFloat(vf)
			}
		}
	case reflect.Bool:
		var vb bool
		if vb, err = strconv.ParseBool(strings.ToLower(strv)); err == nil {
			v.SetBool(vb)
		}
	case reflect.String:
		v.SetString(strv)
	case reflect.Slice:
		slc, ok := vc.getSlice(nameMapper(name))
		if !ok {
			return
		}
		v.Set(reflect.AppendSlice(v, reflect.ValueOf(slc)))
	default:
		err = fmt.Errorf("Cannot store into member %v: unknown type %T", name, v.Interface())
	}
	return
}

func nameMapper(v string) string {
	return strings.ReplaceAll(v, "_", "-")
}
