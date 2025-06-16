// Package group_test is very nearly redundant. I mean, look at it.
package group_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/cobra"
)

func TestAddNavGroup(t *testing.T) {
	n := &cobra.Command{}
	group.AddNavGroup(n)
	if grps := n.Groups(); len(grps) != 1 {
		t.Fatal("incorrect group count.", testsupport.ExpectedActual(1, len(grps)))
	} else if grps[0].ID != group.NavID {
		t.Fatal("incorrect group ID.", testsupport.ExpectedActual(group.NavID, grps[0].ID))
	}
}

func TestAddActionGroup(t *testing.T) {
	a := &cobra.Command{}
	group.AddActionGroup(a)
	if grps := a.Groups(); len(grps) != 1 {
		t.Fatal("incorrect group count.", testsupport.ExpectedActual(1, len(grps)))
	} else if grps[0].ID != group.ActionID {
		t.Fatal("incorrect group ID.", testsupport.ExpectedActual(group.ActionID, grps[0].ID))
	}
}
