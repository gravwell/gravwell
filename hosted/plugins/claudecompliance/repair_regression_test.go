package claudecompliance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCredentialReaderRejectsShortDescriptorRead(t *testing.T) {
	if _, err := readCredential(bytes.NewBufferString("short-key"), 35); err == nil {
		t.Fatal("credential reader accepted fewer bytes than the validated descriptor size")
	}
	if got, err := readCredential(bytes.NewBufferString("complete-key"), int64(len("complete-key"))); err != nil || got != "complete-key" {
		t.Fatalf("credential reader rejected a complete descriptor read: got=%q err=%v", got, err)
	}
	if _, err := readCredential(bytes.NewBuffer(make([]byte, maxCredentialBytes+1)), maxCredentialBytes+1); err == nil {
		t.Fatal("credential reader accepted a descriptor larger than the hard ceiling")
	}
}

func TestNestedRoleAbsenceRetiresPermissionStateOnlyAfterCompleteScans(t *testing.T) {
	p, rt := setup(t, "organizations")
	p.conf.Follow_Children = "enabled"

	type roleState uint
	const (
		rolesPresent roleState = iota
		rolesAbsent
		rolesIncomplete
		rolesFail
	)
	state := rolesPresent
	permissionCalls := map[string]int{}
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			return reply(`{"data":[{"uuid":"O"}],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/users"):
			return reply(`{"data":[],"has_more":false}`, http.StatusOK), nil
		case strings.HasSuffix(r.URL.Path, "/roles"):
			switch state {
			case rolesPresent:
				return reply(`{"data":[{"id":"R"},{"id":"S"}],"has_more":false}`, http.StatusOK), nil
			case rolesAbsent:
				return reply(`{"data":[{"id":"S"}],"has_more":false}`, http.StatusOK), nil
			case rolesIncomplete:
				return reply(`{"data":[{"id":"S"}],"has_more":true,"next_page":"more"}`, http.StatusOK), nil
			case rolesFail:
				return reply(`{"type":"error","error":{"message":"temporary"}}`, http.StatusInternalServerError), nil
			}
		case strings.HasSuffix(r.URL.Path, "/permissions"):
			role := "S"
			if strings.Contains(r.URL.Path, "/roles/R/") {
				role = "R"
			}
			permissionCalls[role]++
			return reply(`{"data":[{"resource_type":"organization","resource_id":"O","action":"read"}],"has_more":false}`, http.StatusOK), nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return nil, nil
	})

	start := p.now()
	cycle := 0
	step := func(wantErr bool) {
		t.Helper()
		cycle++
		at := start.Add(time.Duration(cycle) * 2 * time.Hour)
		p.now = func() time.Time { return at }
		_, err := p.Handle(t.Context(), rt)
		if wantErr && err == nil {
			t.Fatalf("cycle %d: expected request failure", cycle)
		}
		if !wantErr && err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
	}

	for i := 0; i < 4; i++ {
		step(false)
	}
	root, err := p.conf.bind("organizations")
	if err != nil {
		t.Fatal(err)
	}
	workKey := root.key() + keyChildren
	readList := func() worklist {
		t.Helper()
		var list worklist
		if err := json.Unmarshal(mustGet(t, rt, workKey), &list); err != nil {
			t.Fatal(err)
		}
		return list
	}
	const rKey = "role-permissions/organization_id:O/role_id:R"
	const sKey = "role-permissions/organization_id:O/role_id:S"
	rWork, ok := readList().Items[rKey]
	if !ok || rWork.StateKey == "" || permissionCalls["R"] == 0 {
		t.Fatalf("precondition: R permission work was not collected: work=%+v calls=%v", rWork, permissionCalls)
	}
	before := readCheckpoint(t, rt, rWork.StateKey)
	if len(before.Manifest) == 0 {
		t.Fatal("precondition: R permission checkpoint has no manifest state")
	}

	// An incomplete discovered-parent traversal cannot age a missing role.
	state = rolesIncomplete
	p.conf.Max_Pages = 1
	step(false)
	if got := readList().Items[rKey].AbsentStreak; got != 0 {
		t.Fatalf("incomplete role traversal advanced absence streak to %d", got)
	}

	// A failed traversal cannot age it either.
	state = rolesFail
	p.conf.Max_Pages = 5
	step(true)
	if got := readList().Items[rKey].AbsentStreak; got != 0 {
		t.Fatalf("failed role traversal advanced absence streak to %d", got)
	}

	// Reappearance after the interrupted and failed traversals resets any
	// in-progress scan evidence before complete absences are counted.
	state = rolesPresent
	step(false)
	if got := readList().Items[rKey].AbsentStreak; got != 0 {
		t.Fatalf("role reappearance did not reset absence streak: %d", got)
	}

	state = rolesAbsent
	for i := 1; i < absentRetirementThreshold; i++ {
		step(false)
		if got := readList().Items[rKey].AbsentStreak; got != uint(i) {
			t.Fatalf("complete absent traversal %d recorded streak %d", i, got)
		}
	}
	step(false)
	list := readList()
	if _, ok := list.Items[rKey]; ok {
		t.Fatal("R permission work survived the required complete absent traversals")
	}
	if _, ok := list.Items[sKey]; !ok {
		t.Fatal("live sibling S was retired with absent role R")
	}
	after := readCheckpoint(t, rt, rWork.StateKey)
	if len(after.Manifest) != 0 || after.Walk != nil {
		t.Fatalf("absence retirement did not compact R without a tombstone: %+v", after)
	}
	tombstones, err := loadTombstones(rt, p.conf.key()+keyTombstones)
	if err != nil {
		t.Fatal(err)
	}
	if retiredIdentity(tombstones, rKey, rWork.Revision) {
		t.Fatal("absence retirement recorded a deletion tombstone")
	}
}
