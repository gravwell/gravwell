/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import (
	"context"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var _ AcknowledgedWriter = (*NativeRuntime)(nil)
var _ AcknowledgedWriter = (*scopedRuntime)(nil)

// WriteAcknowledged writes one entry through the native per-block
// acknowledgement path. It returns only after an indexer confirms that this
// entry is on disk; unrelated entries in the shared muxer do not participate.
func (nr *NativeRuntime) WriteAcknowledged(ctx context.Context, ent entry.Entry) error {
	if nr == nil || nr.igst == nil {
		return fmt.Errorf("%w, ingest writer not available", ErrNotReady)
	}
	localEnt := ent.DeepCopy()
	if err := nr.igst.WriteEntryAcknowledgedContext(ctx, &localEnt); err != nil {
		return err
	}
	nr.entries.Add(1)
	nr.size.Add(localEnt.Size())
	nr.trackTag(localEnt.Tag)
	return nil
}

func (sr *scopedRuntime) WriteAcknowledged(ctx context.Context, ent entry.Entry) error {
	if err := sr.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	writer, ok := sr.Runtime.(AcknowledgedWriter)
	if !ok {
		return fmt.Errorf("%w, runtime does not provide acknowledged writes", ErrNotReady)
	}
	writeCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(sr.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	if err := writer.WriteAcknowledged(writeCtx, ent); err != nil {
		return err
	}
	// Once the underlying writer acknowledges the entry, late cancellation
	// cannot turn that durable success into a retry and duplicate delivery.
	return nil
}
