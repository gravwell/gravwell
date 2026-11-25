//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package skills

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestSetCommonField(t *testing.T) {
	var cf types.CommonFields
	if inv, err := setCommonField(&cf, "name", "  triage  "); inv != "" || err != nil {
		t.Fatalf("setting a name: invalid=%q err=%v", inv, err)
	}
	if cf.Name != "triage" {
		t.Errorf("name not trimmed: %q", cf.Name)
	}
	if inv, err := setCommonField(&cf, "name", "   "); inv == "" || err != nil {
		t.Errorf("a blank name was accepted: invalid=%q err=%v", inv, err)
	}
	if cf.Name != "triage" {
		t.Errorf("a refused name changed the item: %q", cf.Name)
	}
	if _, err := setCommonField(&cf, "desc", "does things"); err != nil || cf.Description != "does things" {
		t.Errorf("description: %q, %v", cf.Description, err)
	}
	if _, err := setCommonField(&cf, "labels", " a, ,b,"); err != nil || !slices.Equal(cf.Labels, []string{"a", "b"}) {
		t.Errorf("labels: %q, %v", cf.Labels, err)
	}
	if _, err := setCommonField(&cf, "labels", ""); err != nil || cf.Labels == nil || len(cf.Labels) != 0 {
		t.Errorf("clearing labels: %#v, %v", cf.Labels, err)
	}
	if _, err := setCommonField(&cf, "bogus", "x"); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestGetCommonField(t *testing.T) {
	cf := types.CommonFields{Name: "n", Description: "d", Labels: []string{"a", "b"}}
	for key, want := range map[string]string{"name": "n", "desc": "d", "labels": "a,b"} {
		if got, err := getCommonField(cf, key); err != nil || got != want {
			t.Errorf("%s: got %q (%v), want %q", key, got, err, want)
		}
	}
	if _, err := getCommonField(cf, "bogus"); err == nil {
		t.Error("an unknown field was accepted")
	}
}

// metadataPatch must only carry the fields edit manages, so editing does not
// clobber the content, owner or sharing.
func TestMetadataPatch(t *testing.T) {
	cf := types.CommonFields{ID: "x", OwnerID: 3, Name: "n", Description: "d", Labels: []string{}, Readers: types.ACL{Global: true}}
	b, err := json.Marshal(metadataPatch(cf))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"Name": "n", "Description": "d", "Labels": []any{}}
	if len(got) != len(want) {
		t.Fatalf("patch %s, want only Name, Description and Labels", b)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("patch %s is missing %s", b, k)
		}
	}
}
