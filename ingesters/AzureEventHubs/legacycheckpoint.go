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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

// legacyCheckpoint mirrors the on-disk JSON shape written by
// persist.FilePersister in the old, archived azure-event-hubs-go/v3 SDK.
// It's redefined here, rather than importing that module just for this
// struct, since the old SDK is being dropped entirely.
type legacyCheckpoint struct {
	Offset         string    `json:"offset"`
	SequenceNumber int64     `json:"sequenceNumber"`
	EnqueueTime    time.Time `json:"enqueueTime"`
}

// legacyCheckpointPath reproduces persist.FilePersister's getFilePath.
// A single flat file per partition directly under dir, "_"-joined, with
// any "$" (ex: the default consumer group "$Default") stripped.
func legacyCheckpointPath(dir, namespace, eventHub, consumerGroup, partitionID string) string {
	key := strings.Join([]string{namespace, eventHub, consumerGroup, partitionID}, "_")
	key = strings.ReplaceAll(key, "$", "")
	return filepath.Join(dir, key)
}

// eventHubFQDN returns the fully qualified namespace the new SDK derives
// from the connection string built by buildEventHubConnectionString, so
// migrated checkpoints are keyed exactly the ay the SDK/checkpoint store
// look them up.
func eventHubFQDN(hubDef eventHubConf) string {
	return hubDef.Event_Hubs_Namespace + ".servicebus.windows.net"
}

// migrateLegacyCheckpoints copies any checkpoint left behind by the old
// SDK's persist.FilePersister into store, for every partition in
// partitionIDs that doesn't already have a new-format checkpoint.
//
// It's safe to call on every startup. Once a partition has a new-format
// checkpoint (from a prior migration or from normal operation), it's
// never touched again here, so this never overwrites newer progress with
// old/stale data, and costs one directory listing plus a handful of
// stat/read calls per hub once there's nothing left to migrate.
//
// dir is the shared checkpoint root (State_Store_Location). The old SDK
// wrote flat files directly under it and the new store writes
// namespace/hub/consumer-group/partition.json under it, so both formats
// coexist there without colliding.
func migrateLegacyCheckpoints(ctx context.Context, dir string, hubDef eventHubConf, partitionIDs []string, store *fileCheckpointStore, lg *log.KVLogger) {
	fqdn := eventHubFQDN(hubDef)

	existing, err := store.ListCheckpoints(ctx, fqdn, hubDef.Event_Hub, hubDef.Consumer_Group, nil)
	if err != nil {
		lg.Error("failed to list existing checkpoints, skipping legacy checkpoint migration", log.KVErr(err))
		return
	}

	haveCheckpoint := make(map[string]bool, len(existing))
	for _, c := range existing {
		haveCheckpoint[c.PartitionID] = true
	}

	var migrated int
	for _, partitionID := range partitionIDs {
		if haveCheckpoint[partitionID] {
			continue
		}

		path := legacyCheckpointPath(dir, hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, partitionID)
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			lg.Error("failed to read legacy checkpoint", log.KV("partition", partitionID), log.KVErr(err))
			continue
		}

		var legacy legacyCheckpoint
		if err := json.Unmarshal(data, &legacy); err != nil {
			lg.Error("failed to parse legacy checkpoint", log.KV("partition", partitionID), log.KVErr(err))
			continue
		}

		if legacy.Offset == "" {
			continue
		}

		offset := legacy.Offset
		seq := legacy.SequenceNumber
		err = store.SetCheckpoint(ctx, eventhubs.Checkpoint{
			ConsumerGroup:           hubDef.Consumer_Group,
			EventHubName:            hubDef.Event_Hub,
			FullyQualifiedNamespace: fqdn,
			PartitionID:             partitionID,
			Offset:                  &offset,
			SequenceNumber:          &seq,
		}, nil)
		if err != nil {
			lg.Error("failed to migrate legacy checkpoint", log.KV("partition", partitionID), log.KVErr(err))
			continue
		}

		migrated++
		lg.Info("migrated legacy checkpoint", log.KV("partition", partitionID), log.KV("offset", offset))
	}

	if migrated > 0 {
		lg.Info("legacy checkpoint migration complete", log.KV("hub", hubDef.Event_Hub), log.KV("migrated", migrated))
	}
}
