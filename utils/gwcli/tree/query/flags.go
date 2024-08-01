/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

import (
	ft "gwcli/stylesheet/flagtext"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

type queryflags struct {
	duration time.Duration
	script   bool
	json     bool
	csv      bool
	outfn    string
	append   bool
	schedule schedule
	//referenceID string
}

// transmogrifyFlags takes a *parsed* flagset and returns a structured, typed, and (in the case of
// strings) trimmed representation of the flags therein.
// If an error occurs, the current state of the flags will be returned, but may be incomplete.
// While it will coerce data to an appropriate type, transmogrify will *not* check for the state of
// required or dependent flags.
func transmogrifyFlags(fs *pflag.FlagSet) (queryflags, error) {
	var (
		err error
		qf  queryflags
	)

	if qf.duration, err = fs.GetDuration("duration"); err != nil {
		return qf, err
	}
	if qf.script, err = fs.GetBool(ft.Name.Script); err != nil {
		// this will fail if mother is running, it is okay to swallow
		qf.script = false
	}
	if qf.json, err = fs.GetBool(ft.Name.JSON); err != nil {
		return qf, err
	}
	if qf.csv, err = fs.GetBool(ft.Name.CSV); err != nil {
		return qf, err
	}

	if qf.outfn, err = fs.GetString(ft.Name.Output); err != nil {
		return qf, err
	} else {
		qf.outfn = strings.TrimSpace(qf.outfn)
	}
	if qf.append, err = fs.GetBool(ft.Name.Append); err != nil {
		return qf, err
	}

	if qf.schedule.cronfreq, err = fs.GetString(ft.Name.Frequency); err != nil {
		return qf, err
	} else {
		qf.schedule.cronfreq = strings.TrimSpace(qf.schedule.cronfreq)
	}
	if qf.schedule.name, err = fs.GetString(ft.Name.Name); err != nil {
		return qf, err
	} else {
		qf.schedule.name = strings.TrimSpace(qf.schedule.name)
	}
	if qf.schedule.desc, err = fs.GetString(ft.Name.Desc); err != nil {
		return qf, err
	} else {
		qf.schedule.desc = strings.TrimSpace(qf.schedule.desc)
	}

	return qf, nil

}
