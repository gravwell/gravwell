/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// isolateAWSRegion hides any region set by the environment or the shared AWS config.
func isolateAWSRegion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
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
	isolateAWSRegion(t)

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
		{name: "no region", mod: func(v *streamDef) { v.Region = "" }, wantErr: "Region"},
		{name: "no stream name", mod: func(v *streamDef) { v.Stream_Name = "" }, wantErr: "Stream-Name"},
		{name: "timezone override", mod: func(v *streamDef) { v.Timezone_Override = "America/Denver" }, wantTag: "kinesis", wantIter: "LATEST"},
		{name: "invalid timezone", mod: func(v *streamDef) { v.Timezone_Override = "Mars/Olympus" }, wantErr: "Timezone-Override"},
		{name: "both timezones", mod: func(v *streamDef) {
			v.Timezone_Override = "America/Denver"
			v.Assume_Local_Timezone = true
		}, wantErr: "Assume-Local-Timezone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &streamDef{Stream_Name: "stream", Tag_Name: "kinesis", Region: "us-west-1"}
			tt.mod(v)
			err := verifyStream("s", v, loadSDKRegion)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, v.Tag_Name)
			assert.Equal(t, tt.wantIter, v.Iterator_Type)
		})
	}

	assert.ErrorContains(t, verifyStream("s", nil, loadSDKRegion), "nil")
}

func TestVerifyStream_RegionFromEnvironment(t *testing.T) {
	isolateAWSRegion(t)
	t.Setenv("AWS_REGION", "us-east-2")
	assert.NoError(t, verifyStream("s", &streamDef{Stream_Name: "stream", Tag_Name: "kinesis"}, loadSDKRegion))
}

func TestConfig_VerifySDKRegion(t *testing.T) {
	isolateAWSRegion(t)
	t.Setenv("AWS_REGION", "us-east-2")
	c := &cfgType{
		Global: global{
			IngestConfig: config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "secret",
			},
			Credentials_Type: "environment",
		},
		KinesisStream: map[string]*streamDef{
			"a": {Stream_Name: "a", Tag_Name: "kinesis"},
			"b": {Stream_Name: "b", Tag_Name: "kinesis"},
		},
	}
	require.NoError(t, c.Verify())

	t.Setenv("AWS_REGION", "")
	assert.ErrorContains(t, c.Verify(), "Region parameter is empty")
}
