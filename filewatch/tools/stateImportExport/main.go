/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/filewatch"
)

func main() {
	if len(os.Args) != 4 {
		showHelp(os.Args[0])
		return
	}
	switch os.Args[1] {
	case `import`:
		if err := importSet(os.Args[2], os.Args[3]); err != nil {
			fmt.Printf("import failed - %v\n", err)
		}
	case `export`:
		if err := exportSet(os.Args[2], os.Args[3]); err != nil {
			fmt.Printf("export failed - %v\n", err)
		}
	default:
		fmt.Printf("Invalid action %q\n", os.Args[1])
		os.Exit(-1)
	}
}

func showHelp(app string) {
	fmt.Printf("%s <action> <input file> <output file>\n", app)
	fmt.Printf("\nExample Export: %s export /opt/gravwell/etc/file_follow.state /tmp/states.json\n", app)
	fmt.Printf("\nExample Import: %s import /tmp/states.json /opt/gravwell/etc/file_follow.state\n", app)
}

func importSet(input, output string) (err error) {
	var fin *os.File
	var fss []filewatch.FileState
	if fin, err = os.Open(input); err != nil {
		err = fmt.Errorf("failed to open %q %w", input, err)
		return
	}
	dec := json.NewDecoder(fin)
	for {
		var fs filewatch.FileState
		if err = dec.Decode(&fs); err != nil {
			if err == io.EOF {
				err = nil
				break
			}
		}
		fss = append(fss, fs)
	}
	if err = fin.Close(); err != nil {
		err = fmt.Errorf("failed to close input %q %w", input, err)
		return
	}
	err = filewatch.EncodeStateFile(output, fss)

	return
}

func exportSet(input, output string) (err error) {
	var fout *os.File
	var fs []filewatch.FileState
	if fs, err = filewatch.DecodeStateFile(input); err != nil {
		err = fmt.Errorf("failed to decode state file %q - %w", input, err)
		return
	} else if fout, err = os.Create(output); err != nil {
		err = fmt.Errorf("failed to create output file %q - %w", output, err)
		return
	}
	for _, st := range fs {
		if err = writeObject(fout, st); err != nil {
			fout.Close()
			return
		}
	}
	err = fout.Close()
	return
}

func writeObject(wtr io.Writer, obj interface{}) (err error) {
	var bts []byte
	if bts, err = json.Marshal(obj); err != nil {
		err = fmt.Errorf("failed to marshal object %w", err)
	} else if _, err = wtr.Write(bts); err != nil {
		err = fmt.Errorf("failed to write object %w", err)
	} else if _, err = fmt.Fprintf(wtr, "\n"); err != nil {
		err = fmt.Errorf("failed to write object %w", err)
	}
	return
}
