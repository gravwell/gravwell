/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/sqs_common"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	lg = log.NewDiscardLogger()
	os.Exit(m.Run())
}

// logWatcher closes hit when the receive error is logged.
type logWatcher struct {
	hit  chan struct{}
	once sync.Once
}

func (w *logWatcher) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte("sqs receive message error")) {
		w.once.Do(func() { close(w.hit) })
	}
	return len(b), nil
}

func (w *logWatcher) Close() error { return nil }

// receiveGoroutines counts the live goroutines started by queueRunner.
func receiveGoroutines() int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), ".queueRunner.func")
}

// A failed receive must hand over one result and exit (gravwell/issues#3250).
func TestQueueRunner_FailedReceive(t *testing.T) {
	// isolate from the developer's AWS environment
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0600))
	t.Setenv("AWS_CONFIG_FILE", empty)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_MAX_ATTEMPTS", "1")

	w := &logWatcher{hit: make(chan struct{})}
	require.NoError(t, lg.AddWriter(w))
	t.Cleanup(func() { _ = lg.DeleteWriter(w) })

	creds, err := sqs_common.GetCredentials("static", "akid", "secret")
	require.NoError(t, err)

	// nothing listens on port 1, so every receive fails
	s, err := sqs_common.SQSListener(&sqs_common.Config{
		Queue:       "http://127.0.0.1:1/000000000000/q",
		Region:      "us-east-1",
		Endpoint:    "http://127.0.0.1:1",
		Credentials: creds,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	done := make(chan bool)
	wg.Add(1)
	go queueRunner(ctx, &handlerConfig{SQS: s, Name: "test", wg: &wg, done: done, ctx: ctx})

	select {
	case <-w.hit:
	case <-time.After(30 * time.Second):
		t.Fatal("receive did not fail")
	}

	// the runner is now in its error backoff: no receive may be left behind
	require.Eventually(t, func() bool { return receiveGoroutines() == 0 },
		3*time.Second, 10*time.Millisecond, "failed receive leaked a goroutine")

	// a receive in flight at shutdown must also exit
	close(done)
	cancel()
	wg.Wait()
	require.Eventually(t, func() bool { return receiveGoroutines() == 0 },
		3*time.Second, 10*time.Millisecond, "shutdown leaked a receive goroutine")
}
