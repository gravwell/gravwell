/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
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
	"path/filepath"
	"time"

	"github.com/Bowery/prompt"
	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/objlog"
)

var (
	outputDir   = flag.String("output", "", "Output directory")
	server      = flag.String("s", "", "Address and port of Gravwell webserver")
	noCertsEnf  = flag.Bool("insecure", false, "Do NOT enforce webserver certificates, TLS operates in insecure mode")
	noHttps     = flag.Bool("insecure-no-https", false, "Use insecure HTTP connection, passwords are shipped plaintext")
	maxDuration = flag.String("max-duration", "", "maximum duration in the past to export data")

	cutoff time.Time
)

func init() {
	flag.Parse()
	if *outputDir == `` {
		log.Fatal("missing output directory")
	} else if *server == `` {
		log.Fatal("missing server")
	}
	if *maxDuration != `` {
		dur, err := time.ParseDuration(*maxDuration)
		if err != nil {
			log.Fatalf("Failed to parse duration %q - %v\n", *maxDuration, err)
		}
		if dur > 0 {
			dur = dur * -1
		}
		cutoff = time.Now().Add(dur)
	}
}

func main() {
	outDir, err := checkOutputDir(*outputDir)
	if err != nil {
		log.Fatalf("output directory %q is invalid - %v\n", *outputDir, err)
	}
	cli, err := login()
	if err != nil {
		log.Fatalf("Failed to log in to %q: %v\n", *server, err)
	}

	wellData, err := cli.WellData()
	if err != nil {
		log.Fatalf("Failed to retrieve data topologies: %v\n", err)
	}
	wss, err := resolveWellSets(cli, wellData)
	if err != nil {
		log.Fatalf("Failed to resolve well sets: %v\n", err)
	}

	for _, ws := range wss {
		if err := processWell(cli, outDir, ws.name, ws.tags, ws.shards); err != nil {
			fmt.Printf("Failed to process well %s %v\n", ws.name, err)
			break
		}
	}
	cli.Logout()
}

func login() (cli *client.Client, err error) {
	var uname, passwd string
	objLogger, _ := objlog.NewNilLogger()
	if cli, err = client.NewClient(*server, *noCertsEnf, !*noHttps, objLogger); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create new client: %v\n", err)
		return
	}
	if uname, err = prompt.Basic("Username: ", true); err != nil {
		fmt.Fprintf(os.Stderr, "Username error: %v\n", err)
		return
	}
	loggedIn := false
	for i := 0; i < 3; i++ {
		if passwd, err = prompt.Password("Password: "); err != nil {
			fmt.Fprintf(os.Stderr, "Password error: %v\n", err)
			return
		}
		//could not inherit the session, so log in
		if err = cli.Login(uname, passwd); err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			continue
		}
		loggedIn = true
		break
	}
	if !loggedIn {
		return
	}
	if err = cli.TestGet("/"); err != nil {
		fmt.Fprintf(os.Stderr, "TestGet Failed: %v\n", err)
		return
	}

	return
}

func checkOutputDir(dir string) (rdir string, err error) {
	var fi os.FileInfo
	rdir = filepath.Clean(dir)
	if fi, err = os.Stat(rdir); err != nil {
		if !os.IsNotExist(err) {
			return
		}
		//try to make it
		err = os.MkdirAll(rdir, 0700)
		return
	} else if !fi.IsDir() {
		err = fmt.Errorf("%q is not a directory", rdir)
	}
	return
}
