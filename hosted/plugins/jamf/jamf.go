/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package jamf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
// the API reports no more results, then schedules the next run. Each
// configured section is split out to its own tag, with the GENERAL
// section's data folded into every entry.
func (j *Jamf) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	j.initClient(rt.Context())

	tags := make(map[string]entry.EntryTag, len(j.conf.Sections))
	for _, section := range j.conf.Sections {
		tag, err := rt.NegotiateTag(j.conf.tagForSection(section))
		if err != nil {
			return nil, fmt.Errorf("negotiating tag for section %s: %w", section, err)
		}
		tags[section] = tag
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

	if err := j.drain(ctx, rt, tags, filter); err != nil {
		return nil, err
	}

	if err := rt.PutTime(stateKeyLastEnd, end); err != nil {
		rt.Error("failed to persist poll state", log.KVErr(err))
	}

	return j.conf.ContinueAfterInterval(), nil
}

// drain pages through every result for filter, splitting each device record
// into one entry per configured section and writing them until the API
// reports totalCount == 0.
func (j *Jamf) drain(ctx context.Context, rt hosted.Runtime, tags map[string]entry.EntryTag, filter string) error {
	requestSections := j.conf.requestSections()

	for page := 0; ; page++ {
		resp, err := j.c.FetchInventoryPage(ctx, filter, requestSections, page, j.conf.Page_Size)
		if err != nil {
			return fmt.Errorf("fetching page %d: %w", page, err)
		}
		if resp.TotalCount == 0 {
			return nil
		}

		rt.Debug("fetched inventory page", log.KV("page", page), log.KV("count", len(resp.Results)))

		for _, raw := range resp.Results {
			entries, err := splitRecord(raw, j.conf.Sections, tags)
			if err != nil {
				rt.Error("failed to process inventory record", log.KVErr(err))
				continue
			}
			for _, e := range entries {
				if err := rt.Write(e); err != nil {
					rt.Error("failed to write entry", log.KVErr(err))
				}
			}
		}
	}
}

// sectionJSONKey converts a Jamf computers-inventory section constant (e.g.
// "DISK_ENCRYPTION") into the camelCase JSON key Jamf nests that section's
// data under in a record (e.g. "diskEncryption").
func sectionJSONKey(section string) string {
	parts := strings.Split(strings.ToLower(section), "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// splitRecord pulls the GENERAL section's reportDate out of a raw
// computers-inventory record (used as every resulting entry's TS), then
// builds one entry per section in sections that's both configured and
// actually present on this device: {"timestamp", "general", "<section>"}
// for content sections, or just {"timestamp", "general"} for GENERAL
// itself. A section configured but absent from this particular device's
// record is silently skipped for that device.
func splitRecord(raw json.RawMessage, sections []string, tags map[string]entry.EntryTag) ([]entry.Entry, error) {
	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}

	generalRaw, ok := full[sectionJSONKey(generalSectionName)]
	if !ok {
		return nil, errors.New("record missing general section")
	}

	var g struct {
		ReportDate string `json:"reportDate"`
	}
	if err := json.Unmarshal(generalRaw, &g); err != nil {
		return nil, fmt.Errorf("unmarshal general section: %w", err)
	}
	if g.ReportDate == "" {
		return nil, errors.New("record missing general.reportDate")
	}
	ts, err := time.Parse(time.RFC3339Nano, g.ReportDate)
	if err != nil {
		return nil, fmt.Errorf("parsing reportDate %q: %w", g.ReportDate, err)
	}
	tsJSON, err := json.Marshal(g.ReportDate)
	if err != nil {
		return nil, fmt.Errorf("encoding timestamp: %w", err)
	}

	entries := make([]entry.Entry, 0, len(sections))
	for _, section := range sections {
		tag, ok := tags[section]
		if !ok {
			// Every configured section is negotiated up front in Handle;
			// this would only happen if called incorrectly.
			continue
		}

		doc := map[string]json.RawMessage{
			"timestamp": tsJSON,
			"general":   generalRaw,
		}
		if section != generalSectionName {
			key := sectionJSONKey(section)
			sectionRaw, present := full[key]
			if !present {
				// This device has no data for the section; nothing to emit.
				continue
			}
			doc[key] = sectionRaw
		}

		data, err := json.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("marshal %s entry: %w", section, err)
		}
		entries = append(entries, entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: data,
		})
	}
	return entries, nil
}
