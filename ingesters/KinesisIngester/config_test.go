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

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestConfig_VerifyKeepsEnvironment(t *testing.T) {
	t.Setenv("GRAVWELL_INGEST_SECRET", "envsecret")
	t.Setenv("GRAVWELL_CLEARTEXT_TARGETS", "127.0.0.1:4023")
	t.Setenv("GRAVWELL_LOG_LEVEL", "info")
	c := &cfgType{
		Global: global{Credentials_Type: "environment"},
		KinesisStream: map[string]*streamDef{
			"stream1": {Stream_Name: "stream", Tag_Name: "kinesis", Region: "us-west-1"},
		},
	}
	require.NoError(t, c.Verify())
	assert.Equal(t, "envsecret", c.Secret())
	assert.Equal(t, []string{"127.0.0.1:4023"}, c.Global.Cleartext_Backend_Target)
	assert.Equal(t, "INFO", c.LogLevel())
}

func TestVerifyStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mod      func(*streamDef)
		wantErr  string
		wantTag  string
		wantIter string
	}{
		{name: "valid", mod: func(*streamDef) {}, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "default tag", mod: func(v *streamDef) { v.Tag_Name = "" }, wantTag: entry.DefaultTagName, wantIter: "LATEST"},
		{name: "invalid tag", mod: func(v *streamDef) { v.Tag_Name = "bad tag!" }, wantErr: "Tag-Name"},
		{name: "trim horizon", mod: func(v *streamDef) { v.Iterator_Type = "TRIM_HORIZON" }, wantTag: "kinesis", wantIter: "TRIM_HORIZON"},
		{name: "lower case iterator", mod: func(v *streamDef) { v.Iterator_Type = " latest " }, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "unknown iterator", mod: func(v *streamDef) { v.Iterator_Type = "NEWEST" }, wantErr: "Iterator-Type"},
		{name: "iterator needs a timestamp", mod: func(v *streamDef) { v.Iterator_Type = "AT_TIMESTAMP" }, wantErr: "Iterator-Type"},
		{name: "no region is left to the SDK", mod: func(v *streamDef) { v.Region = "" }, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "no stream name", mod: func(v *streamDef) { v.Stream_Name = "" }, wantErr: "Stream-Name"},
		{name: "timezone override", mod: func(v *streamDef) { v.Timezone_Override = "America/Denver" }, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "invalid timezone", mod: func(v *streamDef) { v.Timezone_Override = "Mars/Olympus" }, wantErr: "Timezone-Override"},
		{name: "both timezones", mod: func(v *streamDef) {
			v.Timezone_Override = "America/Denver"
			v.Assume_Local_Timezone = true
		}, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "invalid timezone with local timezone", mod: func(v *streamDef) {
			v.Timezone_Override = "Mars/Olympus"
			v.Assume_Local_Timezone = true
		}, wantErr: "Timezone-Override"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &streamDef{Stream_Name: "stream", Tag_Name: "kinesis", Region: "us-west-1"}
			tt.mod(v)
			err := verifyStream("s", v)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, v.Tag_Name)
			assert.Equal(t, tt.wantIter, v.Iterator_Type)
		})
	}

	assert.ErrorContains(t, verifyStream("s", nil), "nil")
}
