/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
)

const (
	maxFileValueSize int64 = 1024 * 16 // secrets and the like cannot be bigger than 16k when loaded from a file
)

var (
	errNoEnvArg     = errors.New("no env arg")
	ErrInvalidArg   = errors.New("Invalid arguments")
	ErrEmptyEnvFile = errors.New("Environment secret file is empty")
	ErrBadValue     = errors.New("Environment value is invalid")
)

func loadEnvFile(nm string) (r string, err error) {
	var fin *os.File
	if fin, err = os.Open(nm); err != nil {
		// they specified a file but we can't open it
		return
	}
	s := bufio.NewScanner(fin)
	s.Scan()
	if err = s.Err(); err != nil {
		fin.Close()
		return
	}
	r = s.Text()
	if err = fin.Close(); err != nil {
		return
	} else if r == `` {
		// there was nothing in the file?
		err = ErrEmptyEnvFile
	}
	return
}

func loadEnv(nm string) (s string, err error) {
	var ok bool
	if s, ok = os.LookupEnv(nm); ok {
		return
	}

	//try to load the FILE version
	if fp, ok := os.LookupEnv(nm + `_FILE`); ok {
		s, err = loadEnvFile(fp)
	} else {
		err = errNoEnvArg
	}
	return
}

func loadEnvInt(nm string) (v int64, err error) {
	var s string
	if len(nm) == 0 {
		err = ErrInvalidArg
		return
	}
	if s, err = loadEnv(nm); err == nil {
		v, err = ParseInt64(s)
	}
	return
}

func loadEnvUint(nm string) (v uint64, err error) {
	var s string
	if len(nm) == 0 {
		err = ErrInvalidArg
		return
	}
	if s, err = loadEnv(nm); err == nil {
		v, err = ParseUint64(s)
	}
	return
}

// LoadEnvVar attempts to read a value from environment variable named envName
// If there's nothing there, it attempt to append _FILE to the variable
// name and see if it contains a filename; if so, it reads the
// contents of the file into cnd.
func LoadEnvVar(cnd interface{}, envName string, defVal interface{}) error {
	//check that cnd isn't nil, and is a pointer
	if cnd == nil {
		return ErrInvalidArg
	}
	if reflect.ValueOf(cnd).Kind() != reflect.Ptr {
		return ErrInvalidArg
	}

	//this is a partial list, we will load more of these out as we go
	switch v := cnd.(type) {
	case *string:
		var def string
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(string); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarString(v, envName, def)
	case *int:
		var def int
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(int); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarInt(v, envName, def)
	case *uint:
		var def uint
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint(v, envName, def)

	case *int64:
		var def int64
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(int64); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarInt64(v, envName, def)
	case *uint64:
		var def uint64
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint64); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint64(v, envName, def)
	case *int32:
		var def int32
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(int32); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarInt32(v, envName, def)
	case *uint32:
		var def uint32
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint32); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint32(v, envName, def)
	case *uint16:
		var def uint16
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint16); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint16(v, envName, def)
	case *int16:
		var def int16
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(int16); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarInt16(v, envName, def)
	case *uint8:
		var def uint8
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint8); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint8(v, envName, def)
	case *int8:
		var def int8
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(int8); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarInt8(v, envName, def)
	case *bool:
		var def bool
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(bool); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarBool(v, envName, def)
	case *[]string:
		return loadEnvVarList(v, envName)
	}
	return ErrInvalidArg
}

func loadEnvVarBool(cnd *bool, envName string, defVal bool) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd {
		//boolean is already set, exit
		return
	} else if len(envName) == 0 {
		//no environment variable, exit
		return
	}

	var argstr string
	//load the argstr
	if argstr, err = loadEnv(envName); err == errNoEnvArg {
		*cnd = defVal
		err = nil
		return
	}

	//we loaded an argument string, try to parse it
	*cnd, err = ParseBool(argstr)
	return
}

func loadEnvVarInt(cnd *int, envName string, defVal int) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v int64
	if v, err = loadEnvInt(envName); err == nil {
		if v > math.MaxInt || v < math.MinInt {
			err = ErrBadValue
		} else {
			*cnd = int(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarUint(cnd *uint, envName string, defVal uint) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v uint64
	if v, err = loadEnvUint(envName); err == nil {
		if v > math.MaxUint {
			err = ErrBadValue
		} else {
			*cnd = uint(v)
		}
	} else if err == errNoEnvArg {
		*cnd = defVal
		err = nil
	}
	return
}

func loadEnvVarInt64(cnd *int64, envName string, defVal int64) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	if *cnd, err = loadEnvInt(envName); err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarUint64(cnd *uint64, envName string, defVal uint64) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	if *cnd, err = loadEnvUint(envName); err == errNoEnvArg {
		*cnd = defVal
		err = nil
	}
	return
}

func loadEnvVarInt32(cnd *int32, envName string, defVal int32) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v int64
	if v, err = loadEnvInt(envName); err == nil {
		if v > 0x7fffffff || v < -0x7fffffff {
			err = ErrBadValue
		} else {
			*cnd = int32(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarUint32(cnd *uint32, envName string, defVal uint32) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v uint64
	if v, err = loadEnvUint(envName); err == nil {
		if v > 0xffffffff {
			err = ErrBadValue
		} else {
			*cnd = uint32(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarInt16(cnd *int16, envName string, defVal int16) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v int64
	if v, err = loadEnvInt(envName); err == nil {
		if v > 0x7fff || v < -0x7fff {
			err = ErrBadValue
		} else {
			*cnd = int16(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarUint16(cnd *uint16, envName string, defVal uint16) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v uint64
	if v, err = loadEnvUint(envName); err == nil {
		if v > 0xffff {
			err = ErrBadValue
		} else {
			*cnd = uint16(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarInt8(cnd *int8, envName string, defVal int8) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v int64
	if v, err = loadEnvInt(envName); err == nil {
		if v > 0x7f || v < -0x7f {
			err = ErrBadValue
		} else {
			*cnd = int8(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarUint8(cnd *uint8, envName string, defVal uint8) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	var v uint64
	if v, err = loadEnvUint(envName); err == nil {
		if v > 0xff {
			err = ErrBadValue
		} else {
			*cnd = uint8(v)
		}
	} else if err == errNoEnvArg {
		err = nil
		*cnd = defVal
	}
	return
}

func loadEnvVarString(cnd *string, envName, defVal string) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if len(*cnd) > 0 {
		return
	} else if len(envName) == 0 {
		return
	}
	if *cnd, err = loadEnv(envName); err != nil {
		if err == errNoEnvArg {
			err = nil
			*cnd = defVal
		}
	}
	return err
}

func loadEnvVarList(lst *[]string, envName string) error {
	if lst == nil {
		return errors.New("Invalid argument")
	} else if len(*lst) > 0 {
		return nil
	} else if len(envName) == 0 {
		return nil
	}
	arg, err := loadEnv(envName)
	if err == errNoEnvArg {
		err = nil
		arg = ``
	}
	if len(arg) == 0 {
		return nil
	}
	//got something, split it and build our list
	if bits := strings.Split(arg, ","); len(bits) > 0 {
		for _, b := range bits {
			if b = strings.TrimSpace(b); len(b) > 0 {
				*lst = append(*lst, b)
			}
		}
	}
	return nil
}

func loadStringFromFile(pth string, val *string) (err error) {
	if pth == `` {
		return errors.New("invalid path")
	} else if val == nil {
		return errors.New("invalid string pointer")
	}
	// make sure to open and then stat so we don't have some sort of stupid race
	var fin *os.File
	var fi os.FileInfo
	var sz int64
	if fin, err = os.Open(pth); err != nil {
		return
	} else if fi, err = fin.Stat(); err != nil {
		fin.Close()
		return
	} else if !fi.Mode().IsRegular() {
		fin.Close()
		return fmt.Errorf("%q is not a regular file", pth)
	}

	//check the size of the file
	if sz = fi.Size(); sz > maxFileValueSize {
		fin.Close()
		return fmt.Errorf("%q is too large %d", pth, sz)
	}

	//if we hit here, it means we have a size that is ok, load a buffer and read it
	buff := make([]byte, sz)
	_, err = io.ReadFull(fin, buff) //attempt the read
	fin.Close()                     //close the file, because we don't need it anymore
	if err != nil {
		return fmt.Errorf("failed to read complete string from %q %w", pth, err)
	}
	//trim nulls, newlines, and carriage returns from the file because it will be a nightmare to debug
	*val = string(bytes.Trim(buff, "\n\t\r\x00"))

	return
}
