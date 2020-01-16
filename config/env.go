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
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
)

const ()

var (
	errNoEnvArg     = errors.New("no env arg")
	ErrInvalidArg   = errors.New("Invalid arguments")
	ErrEmptyEnvFile = errors.New("Environment secret file is empty")
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

// Attempts to read a value from environment variable named envName
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
	case *uint16:
		var def uint16
		if defVal != nil {
			var ok bool
			if def, ok = defVal.(uint16); !ok {
				return ErrInvalidArg
			}
		}
		return loadEnvVarUint16(v, envName, def)
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
		return
	} else if len(envName) == 0 {
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

func loadEnvVarInt64(cnd *int64, envName string, defVal int64) (err error) {
	if cnd == nil {
		err = ErrInvalidArg
		return
	} else if *cnd != 0 {
		return
	} else if len(envName) == 0 {
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
	*cnd, err = ParseInt64(argstr)
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

	var argstr string
	//load the argstr
	if argstr, err = loadEnv(envName); err == errNoEnvArg {
		*cnd = defVal
		err = nil
		return
	}

	//we loaded an argument string, try to parse it
	*cnd, err = ParseUint64(argstr)
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

	var argstr string
	//load the argstr
	if argstr, err = loadEnv(envName); err == errNoEnvArg {
		*cnd = defVal
		err = nil
		return
	}

	//we loaded an argument string, try to parse it
	var v uint64
	if v, err = ParseUint64(argstr); err == nil {
		if v > 0xffff {
			err = fmt.Errorf("%d overflows uint16", v)
		} else {
			*cnd = uint16(v)
		}
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
