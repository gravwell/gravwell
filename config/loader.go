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
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/ini.v1"
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
	ErrInvalidMapKeyType      = errors.New("invalid map key type, must be string")
	ErrInvalidMapValueType    = errors.New("invalid map value type, must be pointer to struct")
	ErrBadSection             = errors.New("Section has not be initialized")
)

var (
	mpMatch = regexp.MustCompile(`(\S+)\s+(.+)`)
)

type Config struct {
	f *ini.File
}

type Section struct {
	sct      *ini.Section
	sectType string
	sectName string
}

type StructMapper interface {
	MapTo(v interface{}) error
	Keys() []string
	KeyValue(string) (string, error)
}

// LoadConfigFile is a simple wrapper that makes loading a config file directly into a type easier
func LoadFileConfig(p string, v interface{}) error {
	if c, err := OpenFile(p); err != nil {
		return err
	} else if err = c.Import(v); err != nil {
		return err
	}
	return nil
}

func LoadBytesConfig(b []byte, v interface{}) error {
	if c, err := OpenBytes(b); err != nil {
		return err
	} else if err = c.Import(v); err != nil {
		return err
	}
	return nil
}

// OpenBytes will attempt to open a Config using a byte buffer
func OpenBytes(b []byte) (c *Config, err error) {
	var f *ini.File
	if f, err = ini.Load(b); err != nil {
		return
	}
	f.NameMapper = nameMapper
	c = &Config{
		f: f,
	}
	return
}

// OpenFile will open a config file, check the file size, and load the bytes using OpenConfigBytes
// this is a convienence wrapper
func OpenFile(p string) (c *Config, err error) {
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
		return
	} else if err = fin.Close(); err != nil {
		return
	}
	c, err = OpenBytes(bb.Bytes())
	return
}

func (c *Config) GetSections(sectType string) (sects []Section, err error) {
	if c == nil || c.f == nil {
		err = ErrConfigNotOpen
		return
	}
	for _, v := range c.f.Sections() {
		var s Section
		var ok bool
		if s.sectType, s.sectName, ok = sectionMapMapper(v.Name()); !ok {
			s.sectType = v.Name()
		}
		if s.sectType == sectType {
			s.sct = v
			sects = append(sects, s)
		}
	}
	return
}

// Import loads the config file into the given interface
// the parameter must be a pointer to a type
func (c *Config) Import(v interface{}) error {
	if c == nil || c.f == nil {
		return ErrConfigNotOpen
	} else if reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidImportParameter
	} else if v == nil {
		return ErrInvalidImportInterface
	} else if err := c.f.MapTo(v); err != nil {
		return err
	}
	return c.importMaps(v)
}

// importMaps walks the structure using reflection and imports any members that are map types
// if the map is nil we initialize it
// the the goINI package doesn't handle map types for sub sections so we have to do it ourselves
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

func (c *Config) importMap(name string, v reflect.Value) error {
	//ensure the key is of type string
	if v.Type().Key().Kind() != reflect.String {
		return ErrInvalidMapKeyType
	}
	// ensure the value is a pointer to a struct
	if v.Type().Elem().Kind() != reflect.Ptr {
		return ErrInvalidMapValueType
	} else if v.Type().Elem().Elem().Kind() != reflect.Struct {
		return ErrInvalidMapValueType
	}
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	name = strings.ToLower(name)
	mpValType := v.Type().Elem().Elem()
	for _, sect := range c.f.Sections() {
		if sname, skey, ok := sectionMapMapper(sect.Name()); ok && sname == name {
			val := reflect.New(mpValType)
			if err := sect.MapTo(val.Interface()); err != nil {
				return err
			}
			v.SetMapIndex(reflect.ValueOf(skey), val)
		}
	}
	return nil
	//get a list of sections that match this
}

// trying to be clever here where we only lower case the first item
func nameMapper(v string) (r string) {
	return strings.ToLower(strings.ReplaceAll(v, "_", "-"))
}

// map a section name to a map name an key
func sectionMapMapper(v string) (name, key string, ok bool) {
	if mtch := mpMatch.FindStringSubmatch(v); len(mtch) == 3 {
		name = strings.TrimSpace(mtch[1])
		key = strings.Trim(strings.TrimSpace(mtch[2]), `"`)
		ok = len(name) > 0 && len(key) > 0
	}
	return
}

func (s Section) Type() string {
	return s.sectType
}

func (s Section) Name() string {
	return s.sectName
}

func (s Section) String() string {
	if s.sectName != `` {
		return s.sectType + `:` + s.sectName
	}
	return s.sectType
}

func (s Section) Keys() (r []string) {
	if s.sct != nil {
		r = s.sct.KeyStrings()
	}
	return
}

func (s Section) MapTo(v interface{}) (err error) {
	if s.sct == nil {
		err = ErrBadSection
	} else if v == nil {
		err = ErrInvalidImportParameter
	} else {
		err = s.sct.MapTo(v)
	}
	return
}

func (s Section) KeyValue(k string) (val string, err error) {
	var v *ini.Key
	if s.sct == nil {
		err = ErrBadSection
	} else if v == nil {
		err = ErrInvalidImportParameter
	} else if v, err = s.sct.GetKey(k); err == nil {
		val = v.Value()
	}
	return
}
