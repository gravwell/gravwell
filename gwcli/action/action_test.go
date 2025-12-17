/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package action_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	systemshealth "github.com/gravwell/gravwell/v4/gwcli/tree/systems"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user/myinfo"
)

func TestIs(t *testing.T) {

	// create a known action pair
	treePair := myinfo.NewUserMyInfoAction()
	action.AddModel(treePair.Action, treePair.Model)

	if !action.Is(treePair.Action) {
		t.Fatal("known Action classified as Nav")
	}

	if m, err := action.GetModel(treePair.Action); err != nil || m == nil {
		t.Fatal("failed to get model for tree action:", err)
	}

	// create a known nav
	// NOTE(rlandau): this also adds actions underneath this nav to the action map
	statusNav := systemshealth.NewSystemsNav()
	if action.Is(statusNav) {
		t.Fatal("known Nav classified as Action")
	}

	if m, err := action.GetModel(statusNav); err != action.ErrNotAnAction {
		t.Fatal("unexpected error type when getting non-existent model:", err)
	} else if m != nil {
		t.Fatal("GetModel on nav returned a model")
	}

	// panic nil check
	var isNilFn = func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("did not recover despite expected panic")

			}
		}()
		action.Is(nil)
	}
	isNilFn()

	var isUnknownGroupFn = func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("did not recover despite expected panic")

			}
		}()
		treePair.Action.GroupID = "" // empty out the group
		action.Is(treePair.Action)
	}
	isUnknownGroupFn()

}
