/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// registerAndLeave connects an ingester, lets it register, then disconnects it.  What is
// left behind is what the server has on disk, which is what all of this is about.
func registerAndLeave(t *testing.T, h *harness, class string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	m, _ := newManager(t, h, id, class)
	if err := m.RegisterKind(testerKind, false, tester.Config{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the registration to reach the server`, func() bool {
		k, err := h.store.IngesterKinds(id)
		return err == nil && len(k) == 1
	})
	m.Close()
	return id
}

// TestRegistrationsServeWhileNothingIsConnected is the case this exists for.
//
// An ingester registers what it can run and then goes away, which is the ordinary state
// of a fleet being rebooted or of a box that is simply off.  Everything the interface
// needs to answer "what can run here, and what has been given to it" is on disk by then,
// and none of it should depend on a live session.
func TestRegistrationsServeWhileNothingIsConnected(t *testing.T) {
	h := newHarness(t)
	ingester := registerAndLeave(t, h, `edge`)
	waitFor(t, `the ingester to show as gone`, func() bool {
		_, b := h.get(t, `/ui/status`)
		return strings.Contains(b, `no ingesters connected`)
	})

	// a runner created with nothing connected at all
	runner := uuid.New()
	if err := h.store.PutRunner(testerRunner(t, runner, `beat`, `1s`)); err != nil {
		t.Fatal(err)
	}

	// what it can run is still offered
	_, body := h.get(t, `/ui/kinds`)
	if !strings.Contains(body, `Tester`) {
		t.Errorf("the registered kind vanished with the connection:\n%s", body)
	}

	// and the runner says where it is meant to go, rather than that nobody has spoken
	_, body = h.get(t, `/ui/runners`)
	if !strings.Contains(body, `tasked to 1 ingester`) {
		t.Errorf("the runner does not say it is tasked to anything:\n%s", body)
	}
	if !strings.Contains(body, `none connected`) {
		t.Errorf("the runner does not say nothing is connected:\n%s", body)
	}

	// the detail panel names the ingester it is tasked to, even though it has never
	// reported and is not here
	_, body = h.get(t, `/ui/runnerstatus?uuid=`+runner.String())
	if !strings.Contains(body, ingester.String()) {
		t.Errorf("the ingester this runner is tasked to is not listed:\n%s", body)
	}
	if !strings.Contains(body, `not reported`) {
		t.Errorf("a tasked but silent ingester is not marked as such:\n%s", body)
	}
	if !strings.Contains(body, `(offline)`) {
		t.Errorf("the offline ingester is not marked offline:\n%s", body)
	}

	// and the fleet view says how much that box has been given
	_, body = h.get(t, `/ui/ingesters`)
	if !strings.Contains(body, ingester.String()) {
		t.Errorf("a disconnected ingester dropped out of the fleet list:\n%s", body)
	}

	// the assignment pickers are drawn from the same stored registrations
	_, body = h.get(t, `/ui/edit?uuid=`+runner.String())
	if !strings.Contains(body, `<option value="`+ingester.String()+`"`) {
		t.Errorf("a disconnected ingester cannot be assigned to:\n%s", body)
	}
	if !strings.Contains(body, `<option value="edge"`) {
		t.Errorf("the class of a disconnected ingester is not offered:\n%s", body)
	}
}

// TestAssignmentNarrowsTaskingWhileOffline checks the tasking shown is the real rule and
// not just "every ingester we have heard of".
func TestAssignmentNarrowsTaskingWhileOffline(t *testing.T) {
	h := newHarness(t)
	edge := registerAndLeave(t, h, `edge`)
	registerAndLeave(t, h, `cloud`)

	// pinned to one class
	runner := uuid.New()
	rd := testerRunner(t, runner, `beat`, `1s`)
	rd.Assigned = &dynamic.Assignment{Classes: []string{`edge`}}
	if err := h.store.PutRunner(rd); err != nil {
		t.Fatal(err)
	}

	_, body := h.get(t, `/ui/runners`)
	if !strings.Contains(body, `tasked to 1 ingester`) {
		t.Errorf("a class pinned runner is not tasked to exactly the one ingester in it:\n%s", body)
	}
	_, body = h.get(t, `/ui/runnerstatus?uuid=`+runner.String())
	if !strings.Contains(body, edge.String()) {
		t.Errorf("the edge ingester is not listed as tasked:\n%s", body)
	}
	if strings.Count(body, `<tr>`) != 2 { // header plus one row
		t.Errorf("an ingester outside the assignment was listed:\n%s", body)
	}

	// and a runner pinned to a class nothing carries says so rather than sitting silent
	orphan := uuid.New()
	ord := testerRunner(t, orphan, `nowhere`, `1s`)
	ord.Assigned = &dynamic.Assignment{Classes: []string{`does-not-exist`}}
	if err := h.store.PutRunner(ord); err != nil {
		t.Fatal(err)
	}
	_, body = h.get(t, `/ui/runners`)
	if !strings.Contains(body, `no registered ingester matches this assignment`) {
		t.Errorf("a runner nothing can run does not say so:\n%s", body)
	}
}
