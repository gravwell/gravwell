package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWrappedReceiptOrphanRecovery(t *testing.T) {
	runtime := newTestRuntime()
	oldDigest := strings.Repeat("a", 64)
	for sequence := int64(0); sequence < receiptCapacity; sequence++ {
		body, err := json.Marshal(receiptMarker{Sequence: sequence, Generation: 1, Digest: oldDigest})
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Put(receiptSlotKey(sequence), body); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.PutInt64(receiptHeadKey, receiptCapacity); err != nil {
		t.Fatal(err)
	}
	newDigest := strings.Repeat("b", 64)
	orphan, _ := json.Marshal(receiptMarker{Sequence: receiptCapacity, Generation: 2, Digest: newDigest})
	if err := runtime.Put(receiptSlotKey(receiptCapacity), orphan); err != nil {
		t.Fatal(err)
	}

	ledger, err := loadReceiptLedger(runtime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.next != receiptCapacity+1 || !ledger.contains(newDigest) {
		t.Fatalf("ledger next=%d contains orphan=%v", ledger.next, ledger.contains(newDigest))
	}
	if head, err := runtime.GetInt64(receiptHeadKey); err != nil || head != receiptCapacity+1 {
		t.Fatalf("head=%d error=%v", head, err)
	}
}

func TestReceiptLedgerMemoryRemainsBoundedAcrossWrap(t *testing.T) {
	runtime := newTestRuntime()
	ledger, err := loadReceiptLedger(runtime, 1)
	if err != nil {
		t.Fatal(err)
	}
	total := receiptCapacity + 123
	for sequence := int64(0); sequence < total; sequence++ {
		digest := fmt.Sprintf("%064x", sequence)
		if err := ledger.remember(runtime, digest); err != nil {
			t.Fatal(err)
		}
	}
	oldest := fmt.Sprintf("%064x", int64(0))
	newest := fmt.Sprintf("%064x", total-1)
	if len(ledger.seen) != int(receiptCapacity) || len(ledger.slots) != int(receiptCapacity) {
		t.Fatalf("live seen=%d slots=%d want=%d", len(ledger.seen), len(ledger.slots), receiptCapacity)
	}
	if ledger.contains(oldest) || !ledger.contains(newest) {
		t.Fatalf("live oldest=%v newest=%v", ledger.contains(oldest), ledger.contains(newest))
	}

	reloaded, err := loadReceiptLedger(runtime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.seen) != int(receiptCapacity) || len(reloaded.slots) != int(receiptCapacity) {
		t.Fatalf("reloaded seen=%d slots=%d want=%d", len(reloaded.seen), len(reloaded.slots), receiptCapacity)
	}
	if reloaded.contains(oldest) || !reloaded.contains(newest) {
		t.Fatalf("reloaded oldest=%v newest=%v", reloaded.contains(oldest), reloaded.contains(newest))
	}
}

func TestOverlapReceiptLedgerIsIndependentAndReloadable(t *testing.T) {
	runtime := newTestRuntime()
	mainLedger, err := loadReceiptLedger(runtime, 1)
	if err != nil {
		t.Fatal(err)
	}
	overlapLedger, err := loadOverlapReceiptLedger(runtime, 1)
	if err != nil {
		t.Fatal(err)
	}
	mainDigest := strings.Repeat("a", 64)
	overlapDigest := strings.Repeat("b", 64)
	if err := mainLedger.remember(runtime, mainDigest); err != nil {
		t.Fatal(err)
	}
	if err := overlapLedger.remember(runtime, overlapDigest); err != nil {
		t.Fatal(err)
	}
	if mainLedger.contains(overlapDigest) || overlapLedger.contains(mainDigest) {
		t.Fatal("independent receipt namespaces contaminated each other")
	}
	reloaded, err := loadOverlapReceiptLedger(runtime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.contains(overlapDigest) || reloaded.contains(mainDigest) {
		t.Fatal("overlap receipt ledger did not reload independently")
	}
}

func TestCursorLedgerPersistsSeparatelyFromReceiptChurn(t *testing.T) {
	runtime := newTestRuntime()
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err != nil {
		t.Fatal(err)
	}
	cursors, err := loadCursorLedger(runtime, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := cursors.remember(runtime, &state, "cursor-a"); err != nil {
		t.Fatal(err)
	}
	receipts, err := loadReceiptLedger(runtime, state.Generation)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := int64(0); sequence < receiptCapacity+1; sequence++ {
		if err := receipts.remember(runtime, fmt.Sprintf("%064x", sequence)); err != nil {
			t.Fatal(err)
		}
	}
	reloadedState, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadCursorLedger(runtime, reloadedState)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.contains("cursor-a") || reloaded.next != 1 {
		t.Fatalf("cursor marker lost after receipt churn: next=%d contains=%v", reloaded.next, reloaded.contains("cursor-a"))
	}
}

func TestCursorMarkerCapacityCeilingExceedsOneTraversalWindow(t *testing.T) {
	const minimumSupportedTraversalRecords int64 = 10_000_000
	if got := cursorMarkerCapacity * int64(pageSize); got < minimumSupportedTraversalRecords {
		t.Fatalf("cursor marker ceiling=%d records want at least %d", got, minimumSupportedTraversalRecords)
	}

	runtime := newTestRuntime()
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadCursorLedger(runtime, state)
	if err != nil {
		t.Fatal(err)
	}
	for page := int64(0); page < cursorMarkerCapacity; page++ {
		if err := ledger.remember(runtime, &state, fmt.Sprintf("cursor-%d", page)); err != nil {
			t.Fatalf("remember page %d: %v", page, err)
		}
	}
	if err := ledger.remember(runtime, &state, "cursor-overflow"); err == nil || !strings.Contains(err.Error(), "cursor marker capacity") {
		t.Fatalf("capacity error=%v", err)
	}
}

func TestPollStateRejectsCorruption(t *testing.T) {
	for name, state := range map[string]pollState{
		"version":               {Version: 99},
		"negative checkpoint":   {Version: stateVersion, CompletedThrough: -1},
		"negative generation":   {Version: stateVersion, Generation: -1},
		"missing generation":    {Version: stateVersion, Active: true, Oldest: 1, Latest: 2, CursorCycle: 1},
		"missing cursor cycle":  {Version: stateVersion, Active: true, Generation: 1, Oldest: 1, Latest: 2},
		"cursor pages too high": {Version: stateVersion, Active: true, Generation: 1, Oldest: 1, Latest: 2, CursorCycle: 1, CursorPageCount: cursorMarkerCapacity + 1},
		"reversed range":        {Version: stateVersion, Active: true, Generation: 1, Oldest: 3, Latest: 2, CursorCycle: 1},
		"oversized cursor":      {Version: stateVersion, Active: true, Generation: 1, Oldest: 1, Latest: 2, Cursor: strings.Repeat("x", maximumCursorSize+1), CursorCycle: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePollState(state); err == nil {
				t.Fatal("corrupt state accepted")
			}
		})
	}
}

func TestBeginPollStateActivatesLastAndIgnoresPartialInactiveTraversal(t *testing.T) {
	runtime := newTestRuntime()
	runtime.failPutKey = stateActiveKey
	runtime.failPutAt = 1
	runtime.failPutErr = errors.New("activate failed")
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err == nil {
		t.Fatal("begin unexpectedly succeeded")
	}
	loaded, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active || loaded.Oldest != 0 || loaded.Latest != 0 || loaded.CursorCycle != 0 {
		t.Fatalf("partial inactive traversal was resumed: %+v", loaded)
	}
	if loaded.Generation != 1 {
		t.Fatalf("persisted generation=%d want=1", loaded.Generation)
	}

	runtime.failPutKey = ""
	if err := beginPollState(runtime, &loaded, 30, 40); err != nil {
		t.Fatal(err)
	}
	if !loaded.Active || loaded.Generation != 2 || loaded.Oldest != 30 || loaded.Latest != 40 || loaded.CursorCycle != 1 {
		t.Fatalf("retry state=%+v", loaded)
	}
}

func TestCompletePollStateCheckpointsBeforeDeactivation(t *testing.T) {
	runtime := newTestRuntime()
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err != nil {
		t.Fatal(err)
	}
	runtime.failPutKey = stateActiveKey
	runtime.failPutAt = 2
	runtime.failPutErr = errors.New("deactivate failed")
	if err := completePollState(runtime, &state); err == nil {
		t.Fatal("completion unexpectedly succeeded")
	}
	loaded, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Active || loaded.CompletedThrough != 20 || loaded.Latest != 20 {
		t.Fatalf("crash-safe completion state=%+v", loaded)
	}

	runtime.failPutKey = ""
	if err := completePollState(runtime, &loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active || loaded.CompletedThrough != 20 || loaded.Oldest != 0 || loaded.Latest != 0 {
		t.Fatalf("completed state=%+v", loaded)
	}
}

// TestCompletePollStateKeepsTheCheckpointMonotonic covers a backward clock: the
// window is valid, but completing it must retain the existing checkpoint rather
// than persist a lower one.
func TestCompletePollStateKeepsTheCheckpointMonotonic(t *testing.T) {
	runtime := newTestRuntime()
	if err := runtime.PutInt64(stateCompletedThroughKey, 1000); err != nil {
		t.Fatal(err)
	}
	state, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := beginPollState(runtime, &state, 700, 900); err != nil {
		t.Fatal(err)
	}
	if err := completePollState(runtime, &state); err != nil {
		t.Fatal(err)
	}
	if state.CompletedThrough != 1000 {
		t.Fatalf("in-memory checkpoint=%d want=1000", state.CompletedThrough)
	}
	persisted, err := runtime.GetInt64(stateCompletedThroughKey)
	if err != nil {
		t.Fatal(err)
	}
	if persisted != 1000 {
		t.Fatalf("persisted checkpoint=%d want=1000", persisted)
	}

	// A window that really does move forward still advances it.
	loaded, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := beginPollState(runtime, &loaded, 1000, 1200); err != nil {
		t.Fatal(err)
	}
	if err := completePollState(runtime, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.CompletedThrough != 1200 {
		t.Fatalf("forward checkpoint=%d want=1200", loaded.CompletedThrough)
	}
	if persisted, err := runtime.GetInt64(stateCompletedThroughKey); err != nil || persisted != 1200 {
		t.Fatalf("persisted forward checkpoint=%d error=%v", persisted, err)
	}
}

func TestResetCursorStateAdvancesCycleBeforeClearingTraversal(t *testing.T) {
	runtime := newTestRuntime()
	state := newPollState()
	if err := beginPollState(runtime, &state, 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := persistCursorState(runtime, &state, "cursor"); err != nil {
		t.Fatal(err)
	}
	runtime.failPutKey = stateCursorKey
	runtime.failPutAt = 3 // begin, persist, then reset
	runtime.failPutErr = errors.New("cursor reset failed")
	if err := resetCursorState(runtime, &state); err == nil {
		t.Fatal("reset unexpectedly succeeded")
	}
	loaded, err := loadPollState(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CursorCycle != 2 || loaded.Cursor != "cursor" {
		t.Fatalf("partial reset state=%+v", loaded)
	}
}
