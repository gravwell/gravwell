/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package jamf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	Name    string = `jamf`
	ID      string = `jamf.ingesters.gravwell.io`
	Version string = `1.0.0`
)

// stateKeyLastEnd tracks the trailing edge of the last successfully
// processed window, so restarts resume exactly where they left off instead
// of re-fetching or dropping data.
const stateKeyLastEnd = "last-end"

type Jamf struct {
	conf *Config
	c    *Client
}

func New(conf *Config) *Jamf {
	return &Jamf{conf: conf}
}

func (j *Jamf) initClient(ctx context.Context) {
	if j.c == nil {
		j.c = NewClient(ctx, j.conf.Host, j.conf.Client_Id, j.conf.Client_Secret, j.conf.Requests_Per_Minute)
	}
}

// Handle implements hosted.Job. It fetches every computer-inventory record
// whose general.reportDate falls in (lastEnd, now-buffer], paginating until
// the API reports no more results, then schedules the next run.
func (j *Jamf) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	j.initClient(rt.Context())

	tag, err := rt.NegotiateTag(j.conf.Tags()[0])
	if err != nil {
		return nil, fmt.Errorf("negotiating tag: %w", err)
	}

	start, err := hosted.GetTimeOrDefault(rt, stateKeyLastEnd, time.Now().Add(-j.conf.LookbackDuration()))
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}
	end := time.Now().Add(-pollBufferSeconds * time.Second)

	if !end.After(start) {
		// Nothing new to fetch yet; try again next cycle.
		return j.conf.ContinueAfterInterval(), nil
	}

	filter := fmt.Sprintf("general.reportDate>%s;general.reportDate<%s",
		start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))

	if err := j.drain(ctx, rt, tag, filter); err != nil {
		return nil, err
	}

	if err := rt.PutTime(stateKeyLastEnd, end); err != nil {
		rt.Error("failed to persist poll state", log.KVErr(err))
	}

	return j.conf.ContinueAfterInterval(), nil
}

// drain pages through every result for filter, writing each record as an
// entry until the API reports totalCount == 0.
func (j *Jamf) drain(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag, filter string) error {
	for page := 0; ; page++ {
		resp, err := j.c.FetchInventoryPage(ctx, filter, j.conf.Sections, page, j.conf.Page_Size)
		if err != nil {
			return fmt.Errorf("fetching page %d: %w", page, err)
		}
		if resp.TotalCount == 0 {
			return nil
		}

		rt.Debug("fetched inventory page", log.KV("page", page), log.KV("count", len(resp.Results)))

		for _, raw := range resp.Results {
			ts, data, err := stampTimestamp(raw)
			if err != nil {
				rt.Error("failed to process inventory record", log.KVErr(err))
				continue
			}
			e := entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				Data: data,
			}
			if err := rt.Write(e); err != nil {
				rt.Error("failed to write entry", log.KVErr(err))
			}
		}
	}
}

// generalSection is just enough of the computers-inventory record shape to
// pull out the GENERAL section's reportDate field.
type generalSection struct {
	General struct {
		ReportDate string `json:"reportDate"`
	} `json:"general"`
}

// stampTimestamp parses general.reportDate out of a raw computers-inventory
// record and returns both the parsed time (used as the entry's TS) and the
// original record with a top-level "timestamp" field inserted, preserving
// compatibility with dashboards/searches built against the original flow's
// output shape.
func stampTimestamp(raw json.RawMessage) (time.Time, []byte, error) {
	var g generalSection
	if err := json.Unmarshal(raw, &g); err != nil {
		return time.Time{}, nil, fmt.Errorf("unmarshal record: %w", err)
	}
	if g.General.ReportDate == "" {
		return time.Time{}, nil, errors.New("record missing general.reportDate")
	}

	ts, err := time.Parse(time.RFC3339Nano, g.General.ReportDate)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("parsing reportDate %q: %w", g.General.ReportDate, err)
	}

	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return time.Time{}, nil, fmt.Errorf("compacting record: %w", err)
	}
	compact := buf.Bytes()
	if len(compact) == 0 || compact[0] != '{' {
		return time.Time{}, nil, errors.New("record is not a JSON object")
	}

	stamped := make([]byte, 0, len(compact)+len(g.General.ReportDate)+16)
	stamped = append(stamped, []byte(`{"timestamp":"`)...)
	stamped = append(stamped, []byte(g.General.ReportDate)...)
	stamped = append(stamped, []byte(`",`)...)
	stamped = append(stamped, compact[1:]...)

	return ts, stamped, nil
}
