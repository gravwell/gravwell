/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyDefaults(t *testing.T) {
	t.Parallel()
	c := &cfgType{}
	applyDefaults(c)
	if c.Global.Log_File != defaultLogFile {
		t.Errorf("applyDefaults() Log_File = %q, want %q", c.Global.Log_File, defaultLogFile)
	}
	if c.Global.State_Store_Location != defaultStateStore {
		t.Errorf("applyDefaults() State_Store_Location = %q, want %q", c.Global.State_Store_Location, defaultStateStore)
	}

	// explicit values should not be overwritten
	c2 := &cfgType{}
	c2.Global.Log_File = "/custom/log"
	c2.Global.State_Store_Location = "/custom/state"
	applyDefaults(c2)
	if c2.Global.Log_File != "/custom/log" {
		t.Errorf("applyDefaults() overwrote an explicit Log_File: got %q", c2.Global.Log_File)
	}
	if c2.Global.State_Store_Location != "/custom/state" {
		t.Errorf("applyDefaults() overwrote an explicit State_Store_Location: got %q", c2.Global.State_Store_Location)
	}

	// Log-UDP-Target excludes Log-File, so no default
	c3 := &cfgType{}
	c3.Global.Log_UDP_Target = "127.0.0.1:514"
	applyDefaults(c3)
	if c3.Global.Log_File != `` {
		t.Errorf("applyDefaults() set Log_File = %q alongside Log_UDP_Target", c3.Global.Log_File)
	}
}

func TestGetConfigLogDestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		udp  string
		file string // relative to the test's temp dir
	}{
		{name: "Log-UDP-Target", udp: "127.0.0.1:514"},
		{name: "Log-File", file: "kinesis.log"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var logLine, wantFile string
			if tc.udp != `` {
				logLine = "Log-UDP-Target=" + tc.udp
			} else {
				wantFile = filepath.Join(dir, tc.file)
				logLine = "Log-File=" + wantFile
			}
			conf := `[Global]
Ingest-Secret=secret
Pipe-Backend-Target=/opt/gravwell/comms/pipe
` + logLine + `
[KinesisStream "stream"]
	Region=us-west-1
	Tag-Name=kinesis
	Stream-Name=stream
`
			pth := filepath.Join(dir, "c.conf")
			if err := os.WriteFile(pth, []byte(conf), 0600); err != nil {
				t.Fatal(err)
			}
			// GetConfig runs Verify
			c, err := GetConfig(pth, ``)
			if err != nil {
				t.Fatalf("GetConfig() error: %v", err)
			}
			if c.Global.Log_File != wantFile {
				t.Errorf("Log_File = %q, want %q", c.Global.Log_File, wantFile)
			}
			if c.Global.Log_UDP_Target != tc.udp {
				t.Errorf("Log_UDP_Target = %q, want %q", c.Global.Log_UDP_Target, tc.udp)
			}
		})
	}
}
