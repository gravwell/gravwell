/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/stretchr/testify/assert"
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

func TestConfig_VerifyAssumeRole(t *testing.T) {
	t.Parallel()

	const roleARN = "arn:aws:iam::123456789012:role/gravwell-ingest"

	tests := []struct {
		name    string
		global  global
		wantErr string // empty means no error; otherwise a substring of the error
	}{
		{
			name:   "assumerole",
			global: global{Credentials_Type: "assumerole", Role_ARN: roleARN, External_ID: "some-id"},
		},
		{
			name: "assumerole with static source",
			global: global{
				Credentials_Type:        "assumerole",
				Role_ARN:                roleARN,
				Source_Credentials_Type: "static",
				AWS_Access_Key_ID:       "akid",
				AWS_Secret_Access_Key:   "secret",
			},
		},
		{
			name:    "assumerole missing Role-ARN",
			global:  global{Credentials_Type: "assumerole"},
			wantErr: "requires Role-ARN",
		},
		{
			name: "assumerole with keys but no static source",
			global: global{
				Credentials_Type:      "assumerole",
				Role_ARN:              roleARN,
				AWS_Access_Key_ID:     "akid",
				AWS_Secret_Access_Key: "secret",
			},
			wantErr: "access keys are set but not used",
		},
		{
			name:    "Role-ARN without assumerole",
			global:  global{Credentials_Type: "environment", Role_ARN: roleARN},
			wantErr: "Role-ARN requires Credentials-Type=assumerole",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := tt.global
			g.IngestConfig = config.IngestConfig{
				Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
				Ingest_Secret:            "secret",
			}
			conf := cfgType{
				Global: g,
				KinesisStream: map[string]*streamDef{
					"stream1": {Stream_Name: "stream", Tag_Name: "kinesis", Region: "us-west-1"},
				},
			}
			err := conf.Verify()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, "Global credentials invalid")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
