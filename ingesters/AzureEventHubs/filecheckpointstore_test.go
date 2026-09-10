/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"testing"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
)

// Unlike the pure-logic helpers in main_test.go/config_test.go, this store's
// entire job is disk I/O, so these tests exercise the real filesystem via
// t.TempDir() rather than trying to fake it.

func mustNewStore(t *testing.T) *fileCheckpointStore {
	t.Helper()
	store, err := newFileCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	return store
}

func strPtr(s string) *string { return &s }
func i64Ptr(i int64) *int64   { return &i }

func TestFileCheckpointStore_SetAndListCheckpoints_RoundTrip(t *testing.T) {
	store := mustNewStore(t)
	ctx := context.Background()

	cp := eventhubs.Checkpoint{
		ConsumerGroup:           "$Default",
		EventHubName:            "myHub",
		FullyQualifiedNamespace: "myNamespace.servicebus.windows.net",
		PartitionID:             "0",
		Offset:                  strPtr("12345"),
		SequenceNumber:          i64Ptr(42),
	}

	if err := store.SetCheckpoint(ctx, cp, nil); err != nil {
		t.Fatalf("SetCheckpoint() returned error: %v", err)
	}

	got, err := store.ListCheckpoints(ctx, cp.FullyQualifiedNamespace, cp.EventHubName, cp.ConsumerGroup, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListCheckpoints() returned %d checkpoints, want 1", len(got))
	}

	c := got[0]
	if c.PartitionID != cp.PartitionID {
		t.Errorf("PartitionID = %q, want %q", c.PartitionID, cp.PartitionID)
	}
	if c.Offset == nil || *c.Offset != *cp.Offset {
		t.Errorf("Offset = %v, want %v", c.Offset, cp.Offset)
	}
	if c.SequenceNumber == nil || *c.SequenceNumber != *cp.SequenceNumber {
		t.Errorf("SequenceNumber = %v, want %v", c.SequenceNumber, cp.SequenceNumber)
	}
}

func TestFileCheckpointStore_SetCheckpoint_OverwritesPreviousValue(t *testing.T) {
	store := mustNewStore(t)
	ctx := context.Background()

	base := eventhubs.Checkpoint{
		ConsumerGroup:           "$Default",
		EventHubName:            "myHub",
		FullyQualifiedNamespace: "myNamespace.servicebus.windows.net",
		PartitionID:             "0",
	}

	first := base
	first.Offset = strPtr("100")
	if err := store.SetCheckpoint(ctx, first, nil); err != nil {
		t.Fatalf("first SetCheckpoint() returned error: %v", err)
	}

	second := base
	second.Offset = strPtr("200")
	if err := store.SetCheckpoint(ctx, second, nil); err != nil {
		t.Fatalf("second SetCheckpoint() returned error: %v", err)
	}

	got, err := store.ListCheckpoints(ctx, base.FullyQualifiedNamespace, base.EventHubName, base.ConsumerGroup, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListCheckpoints() returned %d checkpoints, want 1 (overwrite, not append)", len(got))
	}
	if got[0].Offset == nil || *got[0].Offset != "200" {
		t.Errorf("Offset = %v, want the latest value %q", got[0].Offset, "200")
	}
}

func TestFileCheckpointStore_ListCheckpoints_NoneYet(t *testing.T) {
	store := mustNewStore(t)
	got, err := store.ListCheckpoints(context.Background(), "ns", "hub", "$Default", nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() on an empty store returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListCheckpoints() on an empty store = %v, want empty", got)
	}
}

func TestFileCheckpointStore_ListCheckpoints_KeepsHubsAndConsumerGroupsSeparate(t *testing.T) {
	store := mustNewStore(t)
	ctx := context.Background()

	for _, cp := range []eventhubs.Checkpoint{
		{FullyQualifiedNamespace: "ns", EventHubName: "hubA", ConsumerGroup: "$Default", PartitionID: "0", Offset: strPtr("1")},
		{FullyQualifiedNamespace: "ns", EventHubName: "hubB", ConsumerGroup: "$Default", PartitionID: "0", Offset: strPtr("2")},
		{FullyQualifiedNamespace: "ns", EventHubName: "hubA", ConsumerGroup: "other", PartitionID: "0", Offset: strPtr("3")},
	} {
		if err := store.SetCheckpoint(ctx, cp, nil); err != nil {
			t.Fatalf("SetCheckpoint() returned error: %v", err)
		}
	}

	got, err := store.ListCheckpoints(ctx, "ns", "hubA", "$Default", nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 || got[0].Offset == nil || *got[0].Offset != "1" {
		t.Errorf("ListCheckpoints(hubA, $Default) = %v, want just the hubA/$Default checkpoint", got)
	}
}

func TestFileCheckpointStore_ClaimAndListOwnership(t *testing.T) {
	store := mustNewStore(t)
	ctx := context.Background()

	requested := []eventhubs.Ownership{
		{FullyQualifiedNamespace: "ns", EventHubName: "hub", ConsumerGroup: "$Default", PartitionID: "0", OwnerID: "me"},
		{FullyQualifiedNamespace: "ns", EventHubName: "hub", ConsumerGroup: "$Default", PartitionID: "1", OwnerID: "me"},
	}

	claimed, err := store.ClaimOwnership(ctx, requested, nil)
	if err != nil {
		t.Fatalf("ClaimOwnership() returned error: %v", err)
	}
	if len(claimed) != len(requested) {
		t.Fatalf("ClaimOwnership() claimed %d, want all %d requested (single-instance store grants everything)", len(claimed), len(requested))
	}

	owned, err := store.ListOwnership(ctx, "ns", "hub", "$Default", nil)
	if err != nil {
		t.Fatalf("ListOwnership() returned error: %v", err)
	}
	if len(owned) != 2 {
		t.Fatalf("ListOwnership() = %d ownerships, want 2", len(owned))
	}

	// a different hub/consumer group should see none of these
	other, err := store.ListOwnership(ctx, "ns", "otherhub", "$Default", nil)
	if err != nil {
		t.Fatalf("ListOwnership() returned error: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("ListOwnership() for an unrelated hub = %v, want empty", other)
	}
}

func TestSanitizePathElement(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"has/slash":   "has_slash",
		"has..dotdot": "has_dotdot",
		"$Default":    "$Default",
	}
	for in, want := range cases {
		if got := sanitizePathElement(in); got != want {
			t.Errorf("sanitizePathElement(%q) = %q, want %q", in, got, want)
		}
	}
}
