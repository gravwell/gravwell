/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"
	"time"
)

// NOTE ON SCOPE: cfgType.Verify() calls c.Global.IngestConfig.Verify() and
// c.Attach.Verify() first, before any of this file's own checks run. Those
// are external gravwell/v3/ingest types whose pass/fail conditions aren't
// visible from here, so a full cfgType.Verify() test would risk failing (or
// silently passing) for reasons that have nothing to do with this ingester's
// own logic. Instead, the checks and defaulting this ingester actually owns
// were pulled out into verifyCheckpointStorage, verifyEventHub, and
// normalizeEventHub specifically so they can be tested directly, independent
// of IngestConfig/AttachConfig. Tags(), Timeout(), parseTimeout(), and the
// simple getters don't touch IngestConfig.Verify()/AttachConfig.Verify()
// either, so they're tested directly against cfgType/global as well.

func TestApplyDefaults(t *testing.T) {
	c := &cfgType{}
	applyDefaults(c)

	if c.Global.Log_File != defaultLogFile {
		t.Errorf("applyDefaults() Log_File = %q, want %q", c.Global.Log_File, defaultLogFile)
	}
	if c.Global.Checkpoint_Storage_Location != defaultCheckpointStorageLocation {
		t.Errorf("applyDefaults() Checkpoint_Storage_Location = %q, want %q",
			c.Global.Checkpoint_Storage_Location, defaultCheckpointStorageLocation)
	}

	// explicit values should not be overwritten
	c2 := &cfgType{}
	c2.Global.Log_File = "/custom/log"
	c2.Global.Checkpoint_Storage_Location = "/custom/checkpoints"
	applyDefaults(c2)
	if c2.Global.Log_File != "/custom/log" {
		t.Errorf("applyDefaults() overwrote an explicit Log_File: got %q", c2.Global.Log_File)
	}
	if c2.Global.Checkpoint_Storage_Location != "/custom/checkpoints" {
		t.Errorf("applyDefaults() overwrote an explicit Checkpoint_Storage_Location: got %q", c2.Global.Checkpoint_Storage_Location)
	}
}

func validEventHub() *eventHubConf {
	return &eventHubConf{
		Event_Hubs_Namespace: "myNamespace",
		Event_Hub:            "myHub",
		Token_Name:           "myPolicy",
		Token_Key:            "s3cr3t",
		Tag_Name:             "mytag",
	}
}

func TestVerifyEventHub(t *testing.T) {
	if err := verifyEventHub("hub1", validEventHub()); err != nil {
		t.Errorf("verifyEventHub() on a fully valid config = %v, want nil", err)
	}

	if err := verifyEventHub("hub1", nil); err == nil {
		t.Errorf("verifyEventHub() with nil config = nil, want error")
	}

	requiredFields := []struct {
		name   string
		mutate func(*eventHubConf)
	}{
		{"Event_Hubs_Namespace", func(v *eventHubConf) { v.Event_Hubs_Namespace = "" }},
		{"Event_Hub", func(v *eventHubConf) { v.Event_Hub = "" }},
		{"Token_Name", func(v *eventHubConf) { v.Token_Name = "" }},
		{"Token_Key", func(v *eventHubConf) { v.Token_Key = "" }},
	}
	for _, tc := range requiredFields {
		t.Run("missing "+tc.name, func(t *testing.T) {
			v := validEventHub()
			tc.mutate(v)
			if err := verifyEventHub("hub1", v); err == nil {
				t.Errorf("verifyEventHub() with empty %s = nil, want error", tc.name)
			}
		})
	}

	checkpointCases := []struct {
		value   string
		wantErr bool
	}{
		{"", false},
		{"start", false},
		{"end", false},
		{"middle", true},
		{"START", true}, // case sensitive, matching the original switch behavior
	}
	for _, tc := range checkpointCases {
		t.Run("Initial_Checkpoint="+tc.value, func(t *testing.T) {
			v := validEventHub()
			v.Initial_Checkpoint = tc.value
			err := verifyEventHub("hub1", v)
			if tc.wantErr && err == nil {
				t.Errorf("verifyEventHub() with Initial_Checkpoint=%q = nil, want error", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("verifyEventHub() with Initial_Checkpoint=%q = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestNormalizeEventHub(t *testing.T) {
	v := &eventHubConf{}
	normalizeEventHub(v)

	if v.Consumer_Group == "" {
		t.Errorf("normalizeEventHub() left Consumer_Group empty")
	}
	if v.Initial_Checkpoint != defaultCheckpoint {
		t.Errorf("normalizeEventHub() Initial_Checkpoint = %q, want %q", v.Initial_Checkpoint, defaultCheckpoint)
	}

	// explicit values should not be overwritten
	v2 := &eventHubConf{Consumer_Group: "mygroup", Initial_Checkpoint: "end"}
	normalizeEventHub(v2)
	if v2.Consumer_Group != "mygroup" {
		t.Errorf("normalizeEventHub() overwrote an explicit Consumer_Group: got %q", v2.Consumer_Group)
	}
	if v2.Initial_Checkpoint != "end" {
		t.Errorf("normalizeEventHub() overwrote an explicit Initial_Checkpoint: got %q", v2.Initial_Checkpoint)
	}
}

func TestTags(t *testing.T) {
	t.Run("no tags at all", func(t *testing.T) {
		c := &cfgType{EventHub: map[string]*eventHubConf{
			"a": {},
		}}
		if _, err := c.Tags(); err == nil {
			t.Errorf("Tags() with no Tag_Name set anywhere = nil error, want error")
		}
	})

	t.Run("dedups repeated tag names", func(t *testing.T) {
		c := &cfgType{EventHub: map[string]*eventHubConf{
			"a": {Tag_Name: "shared"},
			"b": {Tag_Name: "shared"},
			"c": {Tag_Name: "unique"},
		}}
		tags, err := c.Tags()
		if err != nil {
			t.Fatalf("Tags() returned error: %v", err)
		}
		seen := map[string]int{}
		for _, tag := range tags {
			seen[tag]++
		}
		if seen["shared"] != 1 {
			t.Errorf("Tags() included %q %d times, want 1", "shared", seen["shared"])
		}
		if seen["unique"] != 1 {
			t.Errorf("Tags() included %q %d times, want 1", "unique", seen["unique"])
		}
		if len(tags) != 2 {
			t.Errorf("Tags() = %v, want 2 unique tags", tags)
		}
	})
}

func TestParseTimeout(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"empty means zero", "", 0, false},
		{"whitespace only means zero", "   ", 0, false},
		{"valid duration", "30s", 30 * time.Second, false},
		{"invalid duration", "not-a-duration", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &cfgType{}
			c.Global.Connection_Timeout = tc.raw
			got, err := c.parseTimeout()
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseTimeout(%q) = nil error, want error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTimeout(%q) returned unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parseTimeout(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	c := &cfgType{}
	c.Global.Connection_Timeout = "5s"
	if got := c.Timeout(); got != 5*time.Second {
		t.Errorf("Timeout() = %v, want %v", got, 5*time.Second)
	}

	c2 := &cfgType{}
	c2.Global.Connection_Timeout = ""
	if got := c2.Timeout(); got != 0 {
		t.Errorf("Timeout() with no configured timeout = %v, want 0", got)
	}
}

func TestSimpleGetters(t *testing.T) {
	c := &cfgType{}
	c.Global.Ingest_Secret = "shh"
	c.Global.Log_Level = "INFO"
	c.Global.Verify_Remote_Certificates = true

	if got := c.Secret(); got != "shh" {
		t.Errorf("Secret() = %q, want %q", got, "shh")
	}
	if got := c.LogLevel(); got != "INFO" {
		t.Errorf("LogLevel() = %q, want %q", got, "INFO")
	}
	if got := c.VerifyRemote(); got != true {
		t.Errorf("VerifyRemote() = %v, want true", got)
	}
	if got := c.IngestBaseConfig(); got.Ingest_Secret != "shh" {
		t.Errorf("IngestBaseConfig().Ingest_Secret = %q, want %q", got.Ingest_Secret, "shh")
	}
}
