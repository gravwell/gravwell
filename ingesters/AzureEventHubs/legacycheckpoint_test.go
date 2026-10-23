/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

func testLogger() *log.KVLogger {
	return log.NewLoggerWithKV(log.NewDiscardLogger())
}

// writeLegacyCheckpoint writes cp to disk in exactly the shape/location the
// old SDK's persist.FilePersister would have, so migrateLegacyCheckpoints has
// something real to find.
func writeLegacyCheckpoint(t *testing.T, dir, namespace, eventHub, consumerGroup, partitionID string, cp legacyCheckpoint) {
	t.Helper()
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("failed to marshal legacy checkpoint: %v", err)
	}
	path := legacyCheckpointPath(dir, namespace, eventHub, consumerGroup, partitionID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write legacy checkpoint file: %v", err)
	}
}

func TestLegacyCheckpointPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                                          string
		namespace, eventHub, consumerGroup, partition string
		want                                          string
	}{
		{"basic", "ns", "hub", "cg", "0", "ns_hub_cg_0"},
		{"default consumer group strips dollar sign", "ns", "hub", "$Default", "0", "ns_hub_Default_0"},
		{"different partition", "ns", "hub", "$Default", "12", "ns_hub_Default_12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := legacyCheckpointPath("/root", tc.namespace, tc.eventHub, tc.consumerGroup, tc.partition)
			want := filepath.Join("/root", tc.want)
			if got != want {
				t.Errorf("legacyCheckpointPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestEventHubFQDN(t *testing.T) {
	t.Parallel()
	hubDef := eventHubConf{Event_Hubs_Namespace: "myNamespace"}
	got := eventHubFQDN(hubDef)
	want := "myNamespace.servicebus.windows.net"
	if got != want {
		t.Errorf("eventHubFQDN() = %q, want %q", got, want)
	}
}

func TestMigrateLegacyCheckpoints_MigratesLegacyCheckpoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "myNamespace", Event_Hub: "myHub", Consumer_Group: "$Default"}

	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0", legacyCheckpoint{
		Offset:         "12345",
		SequenceNumber: 42,
		EnqueueTime:    time.Now(),
	})

	ctx := context.Background()
	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, eventHubFQDN(hubDef), hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListCheckpoints() after migration = %d checkpoints, want 1", len(got))
	}
	if got[0].PartitionID != "0" {
		t.Errorf("PartitionID = %q, want %q", got[0].PartitionID, "0")
	}
	if got[0].Offset == nil || *got[0].Offset != "12345" {
		t.Errorf("Offset = %v, want %q", got[0].Offset, "12345")
	}
	if got[0].SequenceNumber == nil || *got[0].SequenceNumber != 42 {
		t.Errorf("SequenceNumber = %v, want %d", got[0].SequenceNumber, 42)
	}
}

func TestMigrateLegacyCheckpoints_DoesNotOverwriteExistingNewCheckpoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	fqdn := eventHubFQDN(hubDef)
	ctx := context.Background()

	// A new-format checkpoint already exists, further ahead than the legacy one.
	if err := store.SetCheckpoint(ctx, eventhubs.Checkpoint{
		ConsumerGroup:           hubDef.Consumer_Group,
		EventHubName:            hubDef.Event_Hub,
		FullyQualifiedNamespace: fqdn,
		PartitionID:             "0",
		Offset:                  new("999"),
	}, nil); err != nil {
		t.Fatalf("SetCheckpoint() returned error: %v", err)
	}

	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0", legacyCheckpoint{Offset: "1"})

	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, fqdn, hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 || got[0].Offset == nil || *got[0].Offset != "999" {
		t.Errorf("migration overwrote a newer checkpoint: got %v, want offset unchanged at 999", got)
	}
}

func TestMigrateLegacyCheckpoints_NoLegacyFileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	ctx := context.Background()

	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0", "1"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, eventHubFQDN(hubDef), hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListCheckpoints() with no legacy files to migrate = %v, want empty", got)
	}
}

func TestMigrateLegacyCheckpoints_CorruptLegacyFileSkippedWithoutError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	ctx := context.Background()

	path := legacyCheckpointPath(dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0")
	if err := os.WriteFile(path, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("failed to write corrupt legacy file: %v", err)
	}

	// Must not panic on a corrupt legacy file, and must not fabricate a
	// checkpoint out of it.
	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, eventHubFQDN(hubDef), hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a corrupt legacy file should not produce a migrated checkpoint, got %v", got)
	}
}

func TestMigrateLegacyCheckpoints_EmptyOffsetLegacyFileSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	ctx := context.Background()

	// A well-formed but empty/placeholder legacy checkpoint (offset never
	// actually advanced past nothing) shouldn't be treated as real progress.
	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0", legacyCheckpoint{})

	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, eventHubFQDN(hubDef), hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("an empty-offset legacy file should not produce a migrated checkpoint, got %v", got)
	}
}

func TestMigrateLegacyCheckpoints_ListCheckpointsErrorSkipsWithoutPanicking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}

	// Force ListCheckpoints to hit a real error (not just "not found yet") by
	// putting a plain file where it expects to list a directory.
	parent := filepath.Join(dir, "ns", "hub")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatalf("failed to set up test fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "$Default"), []byte("not a directory"), 0600); err != nil {
		t.Fatalf("failed to set up test fixture: %v", err)
	}

	// Should log the error and return, not panic or hang.
	migrateLegacyCheckpoints(context.Background(), dir, hubDef, []string{"0"}, store, testLogger())
}

func TestMigrateLegacyCheckpoints_OnlyMigratesPartitionsMissingANewCheckpoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	fqdn := eventHubFQDN(hubDef)
	ctx := context.Background()

	// Partition "0" already progressed under the new store; partition "1" never has.
	if err := store.SetCheckpoint(ctx, eventhubs.Checkpoint{
		ConsumerGroup: hubDef.Consumer_Group, EventHubName: hubDef.Event_Hub, FullyQualifiedNamespace: fqdn,
		PartitionID: "0", Offset: new("999"),
	}, nil); err != nil {
		t.Fatalf("SetCheckpoint() returned error: %v", err)
	}

	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0", legacyCheckpoint{Offset: "1"})
	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "1", legacyCheckpoint{Offset: "2"})

	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0", "1"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, fqdn, hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	byPartition := map[string]string{}
	for _, c := range got {
		if c.Offset != nil {
			byPartition[c.PartitionID] = *c.Offset
		}
	}
	if byPartition["0"] != "999" {
		t.Errorf("partition 0 should keep its existing checkpoint untouched, got %q", byPartition["0"])
	}
	if byPartition["1"] != "2" {
		t.Errorf("partition 1 should be migrated from its legacy checkpoint, got %q", byPartition["1"])
	}
}

func TestMigrateLegacyCheckpoints_IdempotentAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := newFileCheckpointStore(dir)
	if err != nil {
		t.Fatalf("newFileCheckpointStore() returned error: %v", err)
	}
	hubDef := eventHubConf{Event_Hubs_Namespace: "ns", Event_Hub: "hub", Consumer_Group: "$Default"}
	fqdn := eventHubFQDN(hubDef)
	ctx := context.Background()

	writeLegacyCheckpoint(t, dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, "0", legacyCheckpoint{Offset: "1"})
	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	// Simulate normal operation advancing the checkpoint past the migrated value.
	if err := store.SetCheckpoint(ctx, eventhubs.Checkpoint{
		ConsumerGroup: hubDef.Consumer_Group, EventHubName: hubDef.Event_Hub, FullyQualifiedNamespace: fqdn,
		PartitionID: "0", Offset: new("50"),
	}, nil); err != nil {
		t.Fatalf("SetCheckpoint() returned error: %v", err)
	}

	// A second migration attempt (e.g. on a restart) must not regress this
	// back to the stale legacy value.
	migrateLegacyCheckpoints(ctx, dir, hubDef, []string{"0"}, store, testLogger())

	got, err := store.ListCheckpoints(ctx, fqdn, hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		t.Fatalf("ListCheckpoints() returned error: %v", err)
	}
	if len(got) != 1 || got[0].Offset == nil || *got[0].Offset != "50" {
		t.Errorf("a second migration call regressed the checkpoint: got %v, want offset 50", got)
	}
}
