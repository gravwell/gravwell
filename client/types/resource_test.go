/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"testing"
)

const testChunk = 64 * 1024

// chunkReader is a deliberately opaque ReadCloser.  It declares Read and Close
// explicitly and embeds nothing, so it cannot promote a WriteTo method from
// whatever it happens to hold.  That matters: io.Copy prefers io.WriterTo, and if
// the source satisfies it then bytes.Buffer.ReadFrom is never called and the
// pre-sizing this file exercises is never exercised at all.  It also hands data
// back in chunks, the way a file or a socket does, rather than in one shot.
//
// If you are tempted to replace this with io.NopCloser or a struct holding a
// *bytes.Reader: don't.  Both expose WriteTo and would quietly neuter
// TestResourceUpdateBytesPresized.  assertNoFastPath below is there to catch it,
// but the type is built this way so the mistake is hard to make in the first place.
type chunkReader struct {
	data  []byte
	reads int
	err   error
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	c.reads++
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := testChunk
	if n > len(p) {
		n = len(p)
	}
	n = copy(p[:n], c.data)
	c.data = c.data[n:]
	return n, nil
}

func (c *chunkReader) Close() error { return nil }

func newChunkReader(b []byte) *chunkReader { return &chunkReader{data: b} }

// hasFastPath reports whether io.Copy would bypass bytes.Buffer.ReadFrom for this
// reader by preferring its WriteTo method.
func hasFastPath(r io.Reader) bool {
	_, ok := r.(io.WriterTo)
	return ok
}

// assertNoFastPath fails if the update's reader would let io.Copy bypass
// bytes.Buffer.ReadFrom.  A reader that implements io.WriterTo turns every
// allocation assertion in this file into a no-op that passes unconditionally, so
// we check rather than trust.
func assertNoFastPath(t *testing.T, ru *ResourceUpdate) {
	t.Helper()
	if hasFastPath(ru.rdr) {
		t.Fatalf("test reader %T implements io.WriterTo: io.Copy will skip Buffer.ReadFrom "+
			"and the pre-size assertions below become meaningless", ru.rdr)
	}
}

// streamUpdate builds a ResourceUpdate backed by an opaque chunked reader and
// verifies up front that the fast path is unavailable.
func streamUpdate(t *testing.T, data []byte, sz uint64) (*ResourceUpdate, *chunkReader) {
	t.Helper()
	rdr := newChunkReader(data)
	ru := &ResourceUpdate{}
	ru.Metadata.Size = sz
	ru.SetStream(rdr)
	assertNoFastPath(t, ru)
	return ru, rdr
}

func TestResourceUpdateBytesData(t *testing.T) {
	ru := ResourceUpdate{Data: []byte(`hello`)}
	b, err := ru.Bytes()
	if err != nil {
		t.Fatal(err)
	} else if string(b) != `hello` {
		t.Fatalf("bad data: %q", b)
	}
}

func TestResourceUpdateBytesStream(t *testing.T) {
	data := bytes.Repeat([]byte(`A`), 4096)
	ru, _ := streamUpdate(t, data, uint64(len(data)))

	b, err := ru.Bytes()
	if err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(b, data) {
		t.Fatalf("bad data: got %d bytes, want %d", len(b), len(data))
	}
}

// a wrong or missing size must not corrupt the result, it only costs us the growth
func TestResourceUpdateBytesBadSize(t *testing.T) {
	data := bytes.Repeat([]byte(`B`), 8192)
	for _, sz := range []uint64{0, 16, uint64(len(data)) * 4, maxResourcePresize + 1} {
		ru, _ := streamUpdate(t, data, sz)
		b, err := ru.Bytes()
		if err != nil {
			t.Fatalf("size %d: %v", sz, err)
		} else if !bytes.Equal(b, data) {
			t.Fatalf("size %d: got %d bytes, want %d", sz, len(b), len(data))
		}
	}
}

// a failed read must surface rather than silently returning a truncated slice
func TestResourceUpdateBytesReadError(t *testing.T) {
	var ru ResourceUpdate
	ru.Metadata.Size = 4096
	ru.SetStream(&chunkReader{err: errors.New("boom")})
	if _, err := ru.Bytes(); err == nil {
		t.Fatal("expected an error from a failing reader")
	}
}

// an empty update has neither Data nor a reader, that must not panic
func TestResourceUpdateBytesEmpty(t *testing.T) {
	var ru ResourceUpdate
	b, err := ru.Bytes()
	if err != nil {
		t.Fatal(err)
	} else if len(b) != 0 {
		t.Fatalf("expected no data, got %d bytes", len(b))
	}
}

// guard on the guard: prove the detector fires on exactly the readers that would
// break the pre-size test, and stays quiet on the one we actually use.  Without
// this, a detector that silently stopped working would take the pre-size
// assertion down with it and nothing would go red.
func TestHasFastPath(t *testing.T) {
	tsts := []struct {
		name string
		rdr  io.Reader
		want bool
	}{
		// the two obvious "simplifications" of chunkReader, both of which
		// expose WriteTo and would neuter TestResourceUpdateBytesPresized
		{`bytes.Reader`, bytes.NewReader([]byte(`data`)), true},
		{`io.NopCloser`, io.NopCloser(bytes.NewReader([]byte(`data`))), true},
		{`bytes.Buffer`, bytes.NewBufferString(`data`), true},
		// what the tests in this file actually use
		{`chunkReader`, newChunkReader([]byte(`data`)), false},
	}
	for _, tst := range tsts {
		if got := hasFastPath(tst.rdr); got != tst.want {
			t.Errorf("%s (%T): hasFastPath = %v, want %v", tst.name, tst.rdr, got, tst.want)
		}
	}
}

// the whole point of the change: reading a stream of known size should not walk a
// doubling ladder.  One allocation for the buffer, not one per rung.
func TestResourceUpdateBytesPresized(t *testing.T) {
	const sz = 4 << 20
	data := bytes.Repeat([]byte(`C`), sz)

	var reads int
	allocs := testing.AllocsPerRun(10, func() {
		ru, rdr := streamUpdate(t, data, sz)
		if _, err := ru.Bytes(); err != nil {
			t.Fatal(err)
		}
		reads = rdr.reads
	})

	// if Read was never called io.Copy took a fast path and measured nothing
	if reads == 0 {
		t.Fatal("reader was never read from, io.Copy bypassed Buffer.ReadFrom")
	}

	// measured: 18 allocs unsized, single digits pre-sized
	if allocs > 6 {
		t.Fatalf("expected a pre-sized read to avoid the growth ladder, got %v allocs", allocs)
	}
}

// allocBytes reports how many bytes fn allocates in one run.  Allocation volume,
// not allocation count, is what actually drives the RSS spikes this change is
// about, and the two do not move together: sizing the buffer to exactly
// Metadata.Size costs one extra allocation but triples the bytes.
func allocBytes(t *testing.T, fn func()) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	fn() // warm up anything lazily initialized so it does not land in the measurement
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// the allocation-count check above cannot see a missing bytes.MinRead: without the
// headroom ReadFrom still has to grow once to make room for the read that returns
// EOF, and that single grow is a full doubling.  One extra allocation, triple the
// bytes.  Measured against a 4MB payload: ~1x with the headroom, ~3x sized exactly,
// ~4x unsized.
func TestResourceUpdateBytesPresizedVolume(t *testing.T) {
	const sz = 4 << 20
	data := bytes.Repeat([]byte(`C`), sz)

	var reads int
	used := allocBytes(t, func() {
		ru, rdr := streamUpdate(t, data, sz)
		b, err := ru.Bytes()
		if err != nil {
			t.Fatal(err)
		} else if len(b) != sz {
			t.Fatalf("short read: %d != %d", len(b), sz)
		}
		reads = rdr.reads
	})

	if reads == 0 {
		t.Fatal("reader was never read from, io.Copy bypassed Buffer.ReadFrom")
	}
	if used > 2*sz {
		t.Fatalf("pre-sized read allocated %dMB for a %dMB payload, expected roughly 1x: "+
			"the buffer is being grown when it should not be",
			used/(1024*1024), sz/(1024*1024))
	}
}
