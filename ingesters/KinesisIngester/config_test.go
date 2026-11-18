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

func TestConfig_Verify_Credentials(t *testing.T) {
	t.Parallel()

	newCfg := func(g global) cfgType {
		g.IngestConfig = config.IngestConfig{
			Cleartext_Backend_Target: []string{"127.0.0.1:4023"},
			Ingest_Secret:            "secret",
		}
		return cfgType{
			Global: g,
			KinesisStream: map[string]*streamDef{
				"stream1": {Stream_Name: "test", Region: "us-east-1", Tag_Name: "kinesis"},
			},
		}
	}

	tests := []struct {
		name    string
		global  global
		wantErr bool
	}{
		{
			name:    "static",
			global:  global{Credentials_Type: "static", AWS_Access_Key_ID: "id", AWS_Secret_Access_Key: "secret"},
			wantErr: false,
		},
		{
			name:    "environment",
			global:  global{Credentials_Type: "environment"},
			wantErr: false,
		},
		{
			name:    "static missing secret",
			global:  global{Credentials_Type: "static", AWS_Access_Key_ID: "id"},
			wantErr: true,
		},
		{
			name:    "invalid type",
			global:  global{Credentials_Type: "foobar"},
			wantErr: true,
		},
		{
			name: "assumerole",
			global: global{
				Credentials_Type: "assumerole",
				Role_ARN:         "arn:aws:iam::123456789012:role/gravwell-ingest",
				External_ID:      "some-id",
			},
			wantErr: false,
		},
		{
			name: "Role-ARN without assumerole",
			global: global{
				Credentials_Type: "environment",
				Role_ARN:         "arn:aws:iam::123456789012:role/gravwell-ingest",
			},
			wantErr: true,
		},
		{
			name:    "assumerole missing Role-ARN",
			global:  global{Credentials_Type: "assumerole"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := newCfg(tt.global).Verify()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
