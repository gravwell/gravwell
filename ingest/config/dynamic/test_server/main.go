/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Command test_server is a stand in webserver for developing and testing the dynamic
// ingester configuration system.  It speaks the server half of the dynamic config RPC
// protocol, keeps what ingesters report in memory, and serves a small web interface for
// creating and editing runner configurations.
//
// It is a development tool, not a product.  It has no user authentication on the web
// interface, so bind it to a loopback address or a trusted network.  Nothing it holds
// outlives the process: stopping it is how you reset it.
//
//	test_server -bind 127.0.0.1:8080 -secret <shared token>
//
// Ingesters connect to ws://<bind>/api/ingesters/control and authenticate with the same
// shared token, which is never transmitted, see the rpc package for how that works.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

var (
	bind   = flag.String(`bind`, `127.0.0.1:8080`, "address:port to serve the HTTP interface on")
	secret = flag.String(`secret`, ``, "shared authentication token, required")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	if *secret == `` {
		return errors.New("-secret is required, it is the shared token ingesters authenticate with")
	}

	lgr, err := log.NewStderrLogger(``)
	if err != nil {
		return fmt.Errorf("failed to build a logger %w", err)
	}
	defer lgr.Close()

	store := NewStore()
	defer store.Close()

	srv, _, err := NewServer(store, *secret, lgr)
	if err != nil {
		return err
	}

	hsrv := &http.Server{
		Addr:              *bind,
		Handler:           srv,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// serve in the background so that a signal can shut it down cleanly and connected
	// ingesters are told rather than left waiting on a socket that has gone
	errCh := make(chan error, 1)
	go func() {
		lgr.Info("serving", log.KV("bind", *bind), log.KV("rpc", client.INGESTERS_CONTROL_URL))
		fmt.Printf("dynamic config test server\n  web interface  http://%s/\n  ingester RPC   ws://%s%s\n  storage        in memory, nothing is persisted\n",
			*bind, *bind, client.INGESTERS_CONTROL_URL)
		if lerr := hsrv.ListenAndServe(); lerr != nil && !errors.Is(lerr, http.ErrServerClosed) {
			errCh <- lerr
		}
		close(errCh)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err = <-errCh:
		if err != nil {
			return fmt.Errorf("failed to serve %w", err)
		}
	case s := <-sig:
		lgr.Info("shutting down", log.KV("signal", s.String()))
	}

	ctx, cf := contextWithTimeout(shutdownTimeout)
	defer cf()
	if err = hsrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shut down cleanly %w", err)
	}
	return nil
}
