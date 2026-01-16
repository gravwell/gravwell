/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gravwell/gravwell/v4/client/types"
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if len(flag.Args()) == 0 {
		fatalf("missing kit manager command argument\n%s\n", commandsStr)
	} else if len(flag.Args()) > 1 {
		log.Fatal("too many arguments provided")
	}

	cmd := flag.Args()[0]
	// make sure we have all the variables we need
	err := initVars(cmd)
	if err != nil {
		fatalf("Error initializing variables: %v\n", err)
	}

	// check which command we are running
	switch cmd {
	case "list": // thing to do here
	case "pull":
		if err = ensureKitDir(); err != nil {
			fatalf("Error with kit directory: %v\n", err)
		}
	case "push":
		if err = ensureKitDir(); err != nil {
			fatalf("Error with kit directory: %v\n", err)
		}
	default:
		fatalf("unknown command '%s'\n%s\n", cmd, commandsStr)
	}

	// get a client fired up
	cli, err := getClient()
	if err != nil {
		fatalf("Error logging into gravwell: %v\n", err)
	}
	defer cli.Close()

	// go get a list of kit builds
	kbrs, err := cli.ListKitBuildHistory()
	if err != nil {
		fatalf("Error getting kit build history: %v\n", err)
	}
	if cmd == `list` {
		printKitList(kbrs)
		return
	}

	// not a list, go make sure the kit specified exists
	// now rip through each one looking for our kit ID
	var kbr types.KitBuildRequest
	for _, v := range kbrs {
		if v.ID == kitId {
			kbr = v
			break
		}
	}
	if kbr.ID != kitId {
		fatalf("Failed to find kit build with ID '%s'\n", kitId)
	}
	switch cmd {
	case `pull`:
		if err = pullKit(cli, kbr); err != nil {
			fatalf("Error syncing kit: %v\n", err)
		}
	case `push`:
		if err = pushKit(cli, kbr); err != nil {
			fatalf("Error deploying kit: %v\n", err)
		}
	default:
		fatalf("unknown command %s\n", cmd)
	}
}

func fatalf(format string, v ...interface{}) {
	fmt.Printf(format, v...)
	os.Exit(1)
}

func fatal(v ...interface{}) {
	fmt.Print(v...)
	os.Exit(1)
}
