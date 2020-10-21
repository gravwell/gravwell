/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
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
	vflag  = flag.Bool("validate", false, "Load configuration file and exit")
)

func ValidateConfig(fnc interface{}, pth string) {
	if !*vflag && !*vpflag {
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
	} else if fnType.NumIn() != 1 {
		fmt.Printf("Given configuration function expects %d parameters instead of 1\n", fnType.NumIn())
		os.Exit(exitCode)
	} else if fnType.NumOut() != 2 {
		fmt.Printf("Given configuration function produces %d output values instead of 2\n", fnType.NumOut())
		os.Exit(exitCode)
	}
	res := fn.Call([]reflect.Value{reflect.ValueOf(pth)})
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
	os.Exit(0) //all good
}
