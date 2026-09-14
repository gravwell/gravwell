/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package jamf

import (
	"reflect"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
)

// TestConfig_Verify checks the required-field, Page-Size, duplicate-section,
// and Tag-Name/Tag-Prefix validation rules enforced by Config.Verify.
func TestConfig_Verify(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "missing host",
			config:  Config{Client_Id: "id", Client_Secret: "secret"},
			wantErr: true,
		},
		{
			name:    "missing client id",
			config:  Config{Host: "https://jamf.example.com", Client_Secret: "secret"},
			wantErr: true,
		},
		{
			name:    "missing client secret",
			config:  Config{Host: "https://jamf.example.com", Client_Id: "id"},
			wantErr: true,
		},
		{
			name: "page size too large",
			config: Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Page_Size: 5000,
			},
			wantErr: true,
		},
		{
			name: "duplicate section",
			config: Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections: []string{"APPLICATIONS", "applications"},
			},
			wantErr: true,
		},
		{
			name: "tag name with multiple sections",
			config: Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections:       []string{"APPLICATIONS", "STORAGE"},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "combined"},
			},
			wantErr: true,
		},
		{
			name: "tag name and tag prefix together",
			config: Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections:       []string{"APPLICATIONS"},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "combined", Tag_Prefix: "custom"},
			},
			wantErr: true,
		},
		{
			name: "valid minimal config",
			config: Config{
				Host: "https://jamf.example.com/", Client_Id: "id", Client_Secret: "secret",
			},
			wantErr: false,
		},
		{
			name: "valid single section with tag name",
			config: Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections:       []string{"APPLICATIONS"},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "jamf_apps"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Verify_Defaults(t *testing.T) {
	c := Config{Host: "https://jamf.example.com/", Client_Id: "id", Client_Secret: "secret"}
	if err := c.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Host != "https://jamf.example.com" {
		t.Errorf("expected trailing slash to be trimmed, got %q", c.Host)
	}
	if c.Page_Size != defaultPageSize {
		t.Errorf("expected default page size %d, got %d", defaultPageSize, c.Page_Size)
	}
	if !reflect.DeepEqual(c.Sections, defaultSections) {
		t.Errorf("expected default sections %v, got %v", defaultSections, c.Sections)
	}
}

func TestConfig_Verify_NormalizesSectionCase(t *testing.T) {
	c := Config{
		Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
		Sections: []string{" applications ", "Disk_Encryption"},
	}
	if err := c.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"APPLICATIONS", "DISK_ENCRYPTION"}
	if !reflect.DeepEqual(c.Sections, want) {
		t.Errorf("expected normalized sections %v, got %v", want, c.Sections)
	}
}

// TestConfig_Tags checks tag derivation: one tag per configured section by
// default, a Tag-Prefix override, and a Tag-Name override for the
// single-section case.
func TestConfig_Tags(t *testing.T) {
	tests := []struct {
		name     string
		sections []string
		tagName  string
		prefix   string
		want     []string
	}{
		{
			name:     "default prefix",
			sections: []string{"APPLICATIONS", "STORAGE"},
			want:     []string{"jamf_applications", "jamf_storage"},
		},
		{
			name:     "custom prefix",
			sections: []string{"APPLICATIONS"},
			prefix:   "custom",
			want:     []string{"custom_applications"},
		},
		{
			name:     "tag name override for single section",
			sections: []string{"APPLICATIONS"},
			tagName:  "jamf_apps_custom",
			want:     []string{"jamf_apps_custom"},
		},
		{
			name:     "general included explicitly",
			sections: []string{"GENERAL", "APPLICATIONS"},
			want:     []string{"jamf_general", "jamf_applications"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections:       tt.sections,
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: tt.tagName, Tag_Prefix: tt.prefix},
			}
			if err := c.Verify(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := c.Tags(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected tags %v, got %v", tt.want, got)
			}
		})
	}
}

func TestConfig_RequestSections(t *testing.T) {
	tests := []struct {
		name     string
		sections []string
		want     []string
	}{
		{
			name:     "general added when absent",
			sections: []string{"APPLICATIONS", "STORAGE"},
			want:     []string{"GENERAL", "APPLICATIONS", "STORAGE"},
		},
		{
			name:     "general not duplicated when present",
			sections: []string{"GENERAL", "APPLICATIONS"},
			want:     []string{"GENERAL", "APPLICATIONS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Sections: tt.sections}
			if got := c.requestSections(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected request sections %v, got %v", tt.want, got)
			}
		})
	}
}
