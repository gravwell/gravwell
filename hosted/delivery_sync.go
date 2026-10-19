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
var _ AcknowledgedWriterUnwrapper = (*scopedRuntime)(nil)

// AcknowledgedWriterFor resolves the acknowledged-delivery capability of a
// Runtime. A plugin that requires durable per-entry delivery calls this before
// it negotiates a tag, reads or writes plugin state, or contacts a vendor, so an
// unsupported runtime is rejected without side effects.
//
// A direct type assertion is not enough. Runtime wrappers embed the Runtime
// interface and declare WriteAcknowledged so they can forward it, which makes
// the assertion succeed even when the wrapped runtime provides nothing and the
// failure only surfaces from the first write. Wrappers implement
// AcknowledgedWriterUnwrapper to answer for what they wrap, and hand back
// themselves so their scoping still applies to every write.
func AcknowledgedWriterFor(rt Runtime) (AcknowledgedWriter, bool) {
	if u, ok := rt.(AcknowledgedWriterUnwrapper); ok {
		writer := u.AcknowledgedWriter()
		return writer, writer != nil
	}
	writer, ok := rt.(AcknowledgedWriter)
	return writer, ok
}

// AcknowledgedWriter implements AcknowledgedWriterUnwrapper. The scoped runtime
// can only acknowledge a write when the runtime it wraps can, and callers write
// through the scoped runtime so the runner's own context keeps bounding them.
// Resolution recurses, so a wrapper around a wrapper still answers for whatever
// is actually at the bottom.
func (sr *scopedRuntime) AcknowledgedWriter() AcknowledgedWriter {
	if sr == nil {
		return nil
	}
	if _, ok := AcknowledgedWriterFor(sr.Runtime); !ok {
		return nil
	}
	return sr
}

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
