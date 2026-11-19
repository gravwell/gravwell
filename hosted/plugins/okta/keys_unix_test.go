//go:build unix

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// opening a FIFO blocks until it has a writer, a Private-Key-File naming one must fail instead of
// hanging the config load
func TestReadKeyFile_FIFO(t *testing.T) {
	t.Parallel()
	pth := filepath.Join(t.TempDir(), "okta.fifo")
	if err := syscall.Mkfifo(pth, 0600); err != nil {
		t.Skipf("cannot create a FIFO: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readKeyFile(pth)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("got %v, want the FIFO rejected", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readKeyFile is blocked opening the FIFO")
	}
}
