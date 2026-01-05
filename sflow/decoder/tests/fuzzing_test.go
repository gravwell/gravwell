/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package tests

import (
	"bytes"
	"embed"
	"testing"

	"github.com/gravwell/gravwell/v3/sflow/decoder"
)

//go:embed *.bin
var tests embed.FS

func FuzzSflowDecoder(f *testing.F) {
	entries, err := tests.ReadDir(".")
	if err != nil {
		f.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := tests.ReadFile(entry.Name())
		if err != nil {
			f.Fatalf("could not read embed fixture bytes %s: %v", entry.Name(), err)
		}
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		d := decoder.NewDatagramDecoder(bytes.NewReader(data))

		// We don't care if it errors, just that it doesn't panic
		_, _ = d.Decode()
	})
}
