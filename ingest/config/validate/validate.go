/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package validate

import (
	"flag"
	"fmt"
	"os"
	"reflect"
)

const (
	exitCode = 255
)

var (
	vflag = flag.Bool("validate", false, "Load configuration file and exit")
)

// ValidateConfig will take a configuration handling function and two paths.
// The first path is the base config file, and the second path points at the conf.d directory.
// The config handling function will take those two paths and process a config and return two values.
// The first value is an opaque object (hence the reflect voodoo).  The second value is an error object
// the point of this function is to make it easy for ingester writers to just hand in their GetConfig function
// and the two paths and get a "go/no go" on the configurations
func ValidateConfig(fnc interface{}, pth, confdPath string) {
	if !*vflag {
		return
	}
	//check the parameters
	if pth == `` {
		fmt.Println("No config filepath provided")
		os.Exit(exitCode)
	} else if fnc == nil {
		fmt.Println("Configuration function is invalid")
		os.Exit(exitCode)
	}
	// do some reflection foo to make sure what we are getting is valid
	fn := reflect.ValueOf(fnc)
	fnType := fn.Type()
	if fnType.Kind() != reflect.Func {
		fmt.Println("Given configuration function is not a function")
		os.Exit(exitCode)
	} else if fnType.NumOut() != 2 {
		fmt.Printf("Given configuration function produces %d output values instead of 2\n", fnType.NumOut())
		os.Exit(exitCode)
	}

	args := []reflect.Value{reflect.ValueOf(pth)}
	if argc := fnType.NumIn(); argc < 1 || argc > 2 {
		fmt.Printf("Given configuration function expects %d parameters instead of 1 or 2\n", argc)
		os.Exit(exitCode)
	} else if argc == 2 {
		args = append(args, reflect.ValueOf(confdPath))
	}
	res := fn.Call(args)
	if len(res) != 2 {
		fmt.Printf("Given configuration function returned the wrong number of values: %d != 2\n", len(res))
		os.Exit(exitCode)
	}
	var err error
	if x := res[1].Interface(); x != nil {
		var ok bool
		if err, ok = res[1].Interface().(error); !ok {
			fmt.Printf("Given configuration function did not return an error type in second value, got %T\n", res[1].Interface())
			os.Exit(exitCode)
		}
	}
	obj := res[0].Interface()
	if err != nil {
		fmt.Printf("Config file %q returned error %v\n", pth, err)
		os.Exit(exitCode)
	} else if obj == nil {
		fmt.Printf("Config file %q returned a nil object\n", pth)
		os.Exit(exitCode)
	}
	if confdPath != `` {
		fmt.Println(pth, "with overlay", confdPath, "is valid")
	} else {
		fmt.Println(pth, "is valid")
	}
	os.Exit(0) //all good
}
