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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
)

// fileCheckpointStore is a local, single-process implementation of
// eventhubs.CheckpointStore. It writes one JSON file per partition checkpoint
// under a root directory (namespace/event-hub/consumer-group/partition.json),
// mirroring how the old persist.FilePersister laid out state on disk.
//
// Ownership claims are always granted in full: this ingester runs a single
// Processor per configured Event Hub with nothing else to coordinate with, so
// there's nothing to arbitrate. If more than one instance of this ingester is
// ever run against the same Event Hub and consumer group, they will NOT
// coordinate partition ownership with each other and will each attempt to
// read every partition.
type fileCheckpointStore struct {
	dir string

	mu         sync.Mutex
	ownerships map[string]eventhubs.Ownership // keyed by namespace|hub|consumergroup|partition
}

// newFileCheckpointStore creates (if needed) the checkpoint root directory
// and returns a store rooted there.
func newFileCheckpointStore(dir string) (*fileCheckpointStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory %q: %w", dir, err)
	}
	return &fileCheckpointStore{
		dir:        dir,
		ownerships: make(map[string]eventhubs.Ownership),
	}, nil
}

// sanitizePathElement keeps namespace/event-hub/consumer-group/partition
// values from escaping the checkpoint directory or colliding with the ".json"
// suffix, in case any of them ever contain path separators.
func sanitizePathElement(s string) string {
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}

func (s *fileCheckpointStore) checkpointDir(namespace, eventHub, consumerGroup string) string {
	return filepath.Join(s.dir,
		sanitizePathElement(namespace),
		sanitizePathElement(eventHub),
		sanitizePathElement(consumerGroup),
	)
}

func (s *fileCheckpointStore) checkpointPath(namespace, eventHub, consumerGroup, partitionID string) string {
	return filepath.Join(s.checkpointDir(namespace, eventHub, consumerGroup), sanitizePathElement(partitionID)+".json")
}

// storedCheckpoint is the on-disk representation of a checkpoint. It's kept
// separate from eventhubs.Checkpoint so the file format doesn't silently
// change if the SDK adds fields to that struct.
type storedCheckpoint struct {
	Offset         *string `json:"offset,omitempty"`
	SequenceNumber *int64  `json:"sequenceNumber,omitempty"`
}

// SetCheckpoint writes a checkpoint to disk via a temp file + rename, so a
// crash mid-write can't corrupt the checkpoint that's already there.
func (s *fileCheckpointStore) SetCheckpoint(ctx context.Context, checkpoint eventhubs.Checkpoint, options *eventhubs.SetCheckpointOptions) error {
	path := s.checkpointPath(checkpoint.FullyQualifiedNamespace, checkpoint.EventHubName, checkpoint.ConsumerGroup, checkpoint.PartitionID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	data, err := json.Marshal(storedCheckpoint{
		Offset:         checkpoint.Offset,
		SequenceNumber: checkpoint.SequenceNumber,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to finalize checkpoint: %w", err)
	}
	return nil
}

// ListCheckpoints returns every checkpoint stored for a given
// namespace/event-hub/consumer-group. A missing directory (no checkpoints
// written yet) is not an error.
func (s *fileCheckpointStore) ListCheckpoints(ctx context.Context, fullyQualifiedNamespace, eventHubName, consumerGroup string, options *eventhubs.ListCheckpointsOptions) ([]eventhubs.Checkpoint, error) {
	dir := s.checkpointDir(fullyQualifiedNamespace, eventHubName, consumerGroup)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}

	var checkpoints []eventhubs.Checkpoint
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		partitionID := strings.TrimSuffix(e.Name(), ".json")

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read checkpoint for partition %s: %w", partitionID, err)
		}
		var sc storedCheckpoint
		if err := json.Unmarshal(data, &sc); err != nil {
			return nil, fmt.Errorf("failed to parse checkpoint for partition %s: %w", partitionID, err)
		}
		checkpoints = append(checkpoints, eventhubs.Checkpoint{
			ConsumerGroup:           consumerGroup,
			EventHubName:            eventHubName,
			FullyQualifiedNamespace: fullyQualifiedNamespace,
			PartitionID:             partitionID,
			Offset:                  sc.Offset,
			SequenceNumber:          sc.SequenceNumber,
		})
	}
	return checkpoints, nil
}

// ClaimOwnership always grants whatever ownership is requested; see the type
// doc comment for why that's safe here.
func (s *fileCheckpointStore) ClaimOwnership(ctx context.Context, partitionOwnership []eventhubs.Ownership, options *eventhubs.ClaimOwnershipOptions) ([]eventhubs.Ownership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	claimed := make([]eventhubs.Ownership, len(partitionOwnership))
	for i, o := range partitionOwnership {
		o.LastModifiedTime = now
		key := ownershipKey(o.FullyQualifiedNamespace, o.EventHubName, o.ConsumerGroup, o.PartitionID)
		s.ownerships[key] = o
		claimed[i] = o
	}
	return claimed, nil
}

// ListOwnership returns the partitions currently tracked as owned for a given
// namespace/event-hub/consumer-group.
func (s *fileCheckpointStore) ListOwnership(ctx context.Context, fullyQualifiedNamespace, eventHubName, consumerGroup string, options *eventhubs.ListOwnershipOptions) ([]eventhubs.Ownership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []eventhubs.Ownership
	for _, o := range s.ownerships {
		if o.FullyQualifiedNamespace == fullyQualifiedNamespace && o.EventHubName == eventHubName && o.ConsumerGroup == consumerGroup {
			out = append(out, o)
		}
	}
	return out, nil
}

func ownershipKey(namespace, eventHub, consumerGroup, partitionID string) string {
	return strings.Join([]string{namespace, eventHub, consumerGroup, partitionID}, "|")
}
