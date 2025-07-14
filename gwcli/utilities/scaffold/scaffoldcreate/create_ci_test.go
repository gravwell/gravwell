//go:build !race
// +build !race

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"fmt"
	"path"
	"strconv"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
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
}

func Test_ExtractValues(t *testing.T) {
	cm := setup(t, Config{
		"A": NewField(true, "A", 0),
		"B": NewField(false, "B", 10),
		"C": NewField(true, "C", -10),
	})

	t.Run("all set", func(t *testing.T) {
		// set values into all TIs
		for i := range cm.orderedTIs {
			cm.orderedTIs[i].ti.SetValue(fmt.Sprintf("%d", i))
		}

		// extract values from TIs
		values, mr := cm.extractValuesFromTIs()
		if len(mr) != 0 {
			t.Errorf("missing required (%v) setting all TIs", mr)
		}
		for key, fld := range cm.fields {
			v, ok := values[key]
			if !ok {
				t.Errorf("failed to find value for key %v (field: %v)", key, fld)
			}
			num, err := strconv.Atoi(v)
			if err != nil {
				t.Errorf("failed to parse %v as an int", v)
			} else if cm.orderedTIs[num].key != key || cm.orderedTIs[num].ti.Value() != v {
				t.Error("mismatching values after extraction.",
					ExpectedActual(cm.orderedTIs[num].key, key),
					ExpectedActual(cm.orderedTIs[num].ti.Value(), v))
			}
		}
	})
	t.Run("missing required", func(t *testing.T) {
		// extract values from TIs
		_, mr := cm.extractValuesFromTIs()
		if len(mr) != 2 {
			t.Error("incorrect missing required count.", ExpectedActual(2, len(mr)))
		}

		// set one of the requireds and try again
		cm.orderedTIs[1].ti.SetValue("test value") // A
		_, mr = cm.extractValuesFromTIs()
		if len(mr) != 1 {
			t.Error("incorrect missing required count.", ExpectedActual(1, len(mr)))
		}

		/*for key, fld := range cm.fields {
			v, ok := values[key]
			if !ok {
				t.Errorf("failed to find value for key %v (field: %v)", key, fld)
			}
			num, err := strconv.Atoi(v)
			if err != nil {
				t.Errorf("failed to parse %v as an int", v)
			} else if cm.orderedTIs[num].key != key || cm.orderedTIs[num].ti.Value() != v {
				t.Error("mismatching values after extraction.",
					ExpectedActual(cm.orderedTIs[num].key, key),
					ExpectedActual(cm.orderedTIs[num].ti.Value(), v))
			}
		}*/
	})
}

func setup(t *testing.T, cfg Config) *createModel {
	t.Helper()
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.NoColor()
	cm := newCreateModel(
		cfg, "test",
		func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		},
		func() pflag.FlagSet { return pflag.FlagSet{} })
	return cm
}
