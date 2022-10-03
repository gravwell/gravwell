// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package wineventlog

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	incEventIDRegex      = regexp.MustCompile(`^\d+$`)
	incEventIDRangeRegex = regexp.MustCompile(`^(\d+)\s*-\s*(\d+)$`)
	excEventIDRegex      = regexp.MustCompile(`^-(\d+)$`)
)

// Query that identifies the source of the events and one or more selectors or
// suppressors.
type Query struct {
	// Name of the channel or the path to the log file that contains the events
	// to query.
	Log string

	IgnoreOlder time.Duration // Ignore records older than this time period.

	// Whitelist and blacklist of event IDs. The value is a comma-separated
	// list. The accepted values are single event IDs to include (e.g. 4634), a
	// range of event IDs to include (e.g. 4400-4500), and single event IDs to
	// exclude (e.g. -4410).
	EventID string

	// Level or levels to include. The value is a comma-separated list of levels
	// to include. The accepted levels are verbose (5), information (4),
	// warning (3), error (2), and critical (1).
	Level string

	// Providers (sources) to include records from.
	Provider []string
}

type queryList struct {
	XMLName xml.Name `xml:"QueryList"`
	Query   query    `xml:"Query"`
}

type query struct {
	XMLName  xml.Name     `xml:"Query"`
	Id       int          `xml:"Id,attr"`
	Select   []selector   `xml:"Select"`
	Suppress []suppressor `xml:"Suppress,omitempty"`
}

type selector struct {
	XMLName xml.Name `xml:"Select"`
	Path    string   `xml:"Path,attr"`
	Body    string   `xml:",innerxml"`
}

type suppressor struct {
	XMLName xml.Name `xml:"Suppress"`
	Path    string   `xml:"Path,attr"`
	Body    string   `xml:",innerxml"`
}

// Build builds a query from the given parameters. The query is returned as a
// XML string and can be used with Subscribe function.
func (q Query) Build() (ret string, err error) {
	if q.Log == "" {
		err = fmt.Errorf("empty log name")
		return
	}

	//get reachback filter
	ignoreOlder := ignoreOlderSelect(q)

	//go get the level selectors
	var levels string
	if levels, err = levelSelect(q); err != nil {
		return
	}

	//go get the providers
	var providers string
	if providers, err = providerSelect(q); err != nil {
		return
	}

	//get event ID inclusions and exclusions
	var includes, excludes []string
	if includes, excludes, err = eventIDSelect(q); err != nil {
		return
	}

	// build up the base of includes with providers levels
	var base string
	if len(ignoreOlder) > 0 {
		base = ignoreOlder
	}
	if len(levels) > 0 {
		if len(base) == 0 {
			base = levels
		} else {
			base = base + " and " + levels
		}
	}
	if len(providers) > 0 {
		if len(base) == 0 {
			base = providers
		} else {
			base = base + " and " + providers
		}
	}

	includeSet := splitStrings(includes, 10)
	excludeSet := splitStrings(excludes, 10)

	ql := queryList{
		Query: query{
			Id: 0,
		},
	}

	// if include and exclude are zero, throw the base and we are done
	if len(includes) == 0 {
		ql.Query.Select = []selector{
			selector{
				Path: q.Log,
				Body: formBody(base, nil),
			},
		}
	} else {
		//otherwise iterate and create a bunch of selectors
		for _, inc := range includeSet {
			ql.Query.Select = append(ql.Query.Select, selector{Path: q.Log, Body: formBody(base, inc)})
		}
	}

	if len(excludeSet) > 0 {
		for _, ex := range excludeSet {
			ql.Query.Suppress = append(ql.Query.Suppress, suppressor{Path: q.Log, Body: formBody(``, ex)})
		}
	}

	//finally render the XML object
	var bts []byte
	if bts, err = xml.Marshal(ql); err == nil {
		ret = string(bts)
	}
	return
}

func splitStrings(set []string, max int) [][]string {
	if len(set) == 0 {
		return nil
	} else if max <= 0 || len(set) < max {
		return [][]string{set}
	}

	var ret [][]string
	for len(set) > 0 {
		if len(set) > max {
			ret = append(ret, set[:max])
			set = set[max:]
		} else {
			ret = append(ret, set)
			set = nil
		}
	}
	return ret
}

func joinSelects(selects []string) (r string) {
	switch len(selects) {
	case 0: //do nothing
	case 1:
		r = selects[0]
	default:
		r = "(" + strings.Join(selects, " or ") + ")"
	}
	return
}

func formBody(base string, selects []string) (r string) {
	var coreStr string
	if len(selects) > 0 {
		if base != `` {
			coreStr = joinSelects(selects) + " and " + base
		} else {
			coreStr = joinSelects(selects)
		}
	} else {
		coreStr = base
	}
	if coreStr == `` {
		r = `*`
	} else {
		r = fmt.Sprintf("*[System[%s]]", coreStr)
	}

	return
}

// queryParams are the parameters that are used to create a query from a
// template.
type queryParams struct {
	Path      string
	Providers []string
	Levels    []string
	EventIDs  []string
	Select    []string
	Suppress  []string
}

func ignoreOlderSelect(q Query) string {
	if q.IgnoreOlder <= 0 {
		return ``
	}

	ms := q.IgnoreOlder.Nanoseconds() / int64(time.Millisecond)
	return fmt.Sprintf("TimeCreated[timediff(@SystemTime) &lt;= %d]", ms)
}

func eventIDSelect(q Query) (includes, excludes []string, err error) {
	if q.EventID == "" {
		return
	}

	components := strings.Split(q.EventID, ",")
	for _, c := range components {
		c = strings.TrimSpace(c)
		switch {
		case incEventIDRegex.MatchString(c):
			includes = append(includes, fmt.Sprintf("EventID=%s", c))
		case excEventIDRegex.MatchString(c):
			m := excEventIDRegex.FindStringSubmatch(c)
			excludes = append(excludes, fmt.Sprintf("EventID=%s", m[1]))
		case incEventIDRangeRegex.MatchString(c):
			m := incEventIDRangeRegex.FindStringSubmatch(c)
			r1, _ := strconv.Atoi(m[1])
			r2, _ := strconv.Atoi(m[2])
			if r1 >= r2 {
				err = fmt.Errorf("event ID range '%s' is invalid", c)
				return
			}
			includes = append(includes,
				fmt.Sprintf("(EventID &gt;= %d and EventID &lt;= %d)", r1, r2))
		default:
			err = fmt.Errorf("invalid event ID query component ('%s')", c)
			return
		}
	}
	return
	/*
		if len(includes) == 1 {
			qp.Select = []Select{append(qp.Select, includes...)}
		} else if len(includes) > 1 {
			qp.Select = append(qp.Select, "("+strings.Join(includes, " or ")+")")
		}
		if len(excludes) == 1 {
			qp.Suppress = append(qp.Suppress, excludes...)
		} else if len(excludes) > 1 {
			qp.Suppress = append(qp.Suppress, "("+strings.Join(excludes, " or ")+")")
		}
	*/
}

// levelSelect returns a xpath selector for the event Level. The returned
// selector will select events with levels less than or equal to the specified
// level. Note that level 0 is used as a catch-all/unknown level.
//
// Accepted levels:
//  verbose           - 5
//  information, info - 4 or 0
//  warning,     warn - 3
//  error,       err  - 2
//  critical,    crit - 1
func levelSelect(q Query) (levels string, err error) {
	if q.Level == "" {
		return
	}

	l := func(level int) string { return fmt.Sprintf("Level = %d", level) }

	var levelSelect []string
	for _, expr := range strings.Split(q.Level, ",") {
		expr = strings.TrimSpace(expr)
		switch strings.ToLower(expr) {
		default:
			err = fmt.Errorf("invalid level ('%s') for query", q.Level)
			return
		case "verbose", "5":
			levelSelect = append(levelSelect, l(5))
		case "information", "info", "4":
			levelSelect = append(levelSelect, l(0), l(4))
		case "warning", "warn", "3":
			levelSelect = append(levelSelect, l(3))
		case "error", "err", "2":
			levelSelect = append(levelSelect, l(2))
		case "critical", "crit", "1":
			levelSelect = append(levelSelect, l(1))
		case "0":
			levelSelect = append(levelSelect, l(0))
		}
	}
	if len(levelSelect) > 0 {
		levels = "(" + strings.Join(levelSelect, " or ") + ")"
	}
	return
}

func providerSelect(q Query) (providers string, err error) {
	if len(q.Provider) == 0 {
		return
	}

	var lst []string
	for _, p := range q.Provider {
		lst = append(lst, fmt.Sprintf("@Name='%s'", p))
	}
	if len(lst) > 0 {
		providers = fmt.Sprintf("Provider[%s]", strings.Join(lst, " or "))
	}
	return
}
