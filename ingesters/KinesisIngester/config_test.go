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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/stretchr/testify/assert"
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
Credentials-Type=environment
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

func TestConfig_VerifyCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		credTyp string
		akid    string
		secret  string
		wantErr bool
	}{
		{name: "static", credTyp: "static", akid: "akid", secret: "secret"},
		{name: "empty type is static", akid: "akid", secret: "secret"},
		{name: "environment", credTyp: "environment"},
		{name: "ec2role", credTyp: "ec2role"},
		{name: "invalid type", credTyp: "ec2-role", wantErr: true},
		{name: "empty type without keys", wantErr: true},
		{name: "static without ID", credTyp: "static", secret: "secret", wantErr: true},
		{name: "static without secret", credTyp: "static", akid: "akid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conf := cfgType{
				Global: global{
					IngestConfig: config.IngestConfig{
						Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
						Ingest_Secret:            "secret",
					},
					Credentials_Type:      tt.credTyp,
					AWS_Access_Key_ID:     tt.akid,
					AWS_Secret_Access_Key: tt.secret,
				},
				KinesisStream: map[string]*streamDef{
					"stream1": {Stream_Name: "stream", Tag_Name: "kinesis", Region: "us-west-1"},
				},
			}
			err := conf.Verify()
			if tt.wantErr {
				assert.ErrorContains(t, err, "Global credentials invalid")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
