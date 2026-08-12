/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package jamf

import (
	"slices"
	"testing"
)

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
			name: "valid minimal config",
			config: Config{
				Host: "https://jamf.example.com/", Client_Id: "id", Client_Secret: "secret",
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
	if !slices.Equal(c.Sections, defaultSections) {
		t.Errorf("expected default sections %v, got %v", defaultSections, c.Sections)
	}
	if got := c.Tags(); len(got) != 1 || got[0] != defaultTag {
		t.Errorf("expected default tag %q, got %v", defaultTag, got)
	}
}

func TestConfig_Verify_Sections(t *testing.T) {
	tests := []struct {
		name         string
		sections     []string
		wantSections []string
	}{
		{
			name:         "nil sections get GENERAL defaults",
			sections:     nil,
			wantSections: defaultSections,
		},
		{
			name:         "empty sections get GENERAL defaults",
			sections:     []string{},
			wantSections: defaultSections,
		},
		{
			name:         "user sections without GENERAL get it appended",
			sections:     []string{"STORAGE"},
			wantSections: []string{"STORAGE", sectionGeneral},
		},
		{
			name:         "user sections already containing GENERAL are unchanged",
			sections:     []string{sectionGeneral, "STORAGE"},
			wantSections: []string{sectionGeneral, "STORAGE"},
		},
		{
			name:         "user sections with GENERAL not first are unchanged",
			sections:     []string{"STORAGE", sectionGeneral, "DISK_ENCRYPTION"},
			wantSections: []string{"STORAGE", sectionGeneral, "DISK_ENCRYPTION"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				Host: "https://jamf.example.com", Client_Id: "id", Client_Secret: "secret",
				Sections: tt.sections,
			}
			if err := c.Verify(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(c.Sections, tt.wantSections) {
				t.Errorf("Sections = %v, want %v", c.Sections, tt.wantSections)
			}
			if !slices.Contains(c.Sections, sectionGeneral) {
				t.Errorf("Sections %v does not contain %q", c.Sections, sectionGeneral)
			}
			// Guard against GENERAL being duplicated by a broken slices.Contains check.
			var generalCount int
			for _, s := range c.Sections {
				if s == sectionGeneral {
					generalCount++
				}
			}
			if generalCount != 1 {
				t.Errorf("Sections %v contains %q %d times, want exactly 1", c.Sections, sectionGeneral, generalCount)
			}
		})
	}
}
