/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"testing"

	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/pflag"
)

func Test_createModel_basics(t *testing.T) {
	cfg := map[string]Field{
		"A": NewField(true, "A", 0),
		"B": NewField(true, "B", 0),
	}
	cm := newCreateModel(cfg, "test",
		func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		}, func() pflag.FlagSet {
			return pflag.FlagSet{}
		})

	if len(cm.orderedTIs) != 2 {
		t.Fatal(ExpectedActual(1, len(cm.orderedTIs)))
	} else if cm.orderedTIs[0].key != "A" {
		t.Fatal(ExpectedActual("A", cm.orderedTIs[0].key))
	} else if cm.orderedTIs[1].key != "B" {
		t.Fatal(ExpectedActual("B", cm.orderedTIs[1].key))
	}
	cm.focusNext()
	// should be the second field
	if cm.selected != 1 {
		t.Fatal("expected second field to be selected")
	}
	cm.focusNext()
	// should be the submit button
	if cm.selected != uint(len(cm.orderedTIs)) {
		t.Fatal("expected submit button to be selected")
	}
	cm.focusNext()
	// should be the first field
	if cm.selected != 0 {
		t.Fatal("expected first field to be selected")
	}
	cm.focusPrevious()
	// should be the submit button
	if cm.selected != uint(len(cm.orderedTIs)) {
		t.Fatal("expected submit button to be selected")
	}

	/*type fields struct {
		mode               mode
		width              int
		singular           string
		fields             Config
		orderedTIs         []keyedTI
		selected           uint
		longestFieldLength int
		inputErr           string
		createErr          string
		addtlFlagFunc      func() pflag.FlagSet
		fs                 pflag.FlagSet
		cf                 CreateFunc
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &createModel{
				mode:               tt.fields.mode,
				width:              tt.fields.width,
				singular:           tt.fields.singular,
				fields:             tt.fields.fields,
				orderedTIs:         tt.fields.orderedTIs,
				selected:           tt.fields.selected,
				longestFieldLength: tt.fields.longestFieldLength,
				inputErr:           tt.fields.inputErr,
				createErr:          tt.fields.createErr,
				addtlFlagFunc:      tt.fields.addtlFlagFunc,
				fs:                 tt.fields.fs,
				cf:                 tt.fields.cf,
			}
			c.focusPrevious()
		})
	}*/
}
