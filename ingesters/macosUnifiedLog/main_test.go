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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

// TestRunStreamsPartialStartupFailure is a regression test: if any stream
// fails to resolve its tag, no other stream's `log` process should ever be
// started, and runStreams should report the failure as an error rather than
// exiting the process out from under an already-running stream.
func TestRunStreamsPartialStartupFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh available")
	}

	// stands in for `log`: if ever started, it touches a sentinel file and then
	// blocks, so a wrongly-started process would be easy to catch mid-run
	sentinel := filepath.Join(t.TempDir(), "started")
	script := filepath.Join(t.TempDir(), "fake-log")
	body := fmt.Sprintf("#!/bin/sh\ntouch %q\nexec sleep 60\n", sentinel)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	orig := logCommand
	logCommand = script
	t.Cleanup(func() { logCommand = orig })

	// the muxer only knows about "ok", so GetTag fails for stream "b" without
	// ever dialing out
	muxer, err := ingest.NewUniformMuxer(ingest.UniformMuxerConfig{
		Destinations: []string{"127.0.0.1:0"},
		Tags:         []string{"ok"},
		Auth:         "testing",
		Logger:       log.NewDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("failed to build test muxer: %v", err)
	}

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			"a": {Tag_Name: "ok"},
			"b": {Tag_Name: "missing"},
		},
	}

	out := &syncBuffer{}
	lg := log.New(out)

	err = runStreams(context.Background(), cfg, muxer, "", lg)
	if err == nil {
		t.Fatal("expected an error from runStreams, got nil")
	}

	// give a wrongly-started process a moment to touch the sentinel
	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("stream %q's log process was started despite stream %q failing to resolve its tag", "a", "b")
	}
}
