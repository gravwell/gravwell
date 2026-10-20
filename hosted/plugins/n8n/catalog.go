/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"fmt"
	"sort"
	"strings"
)

type datasetDefinition struct {
	Name         string
	TagKind      string
	Method       string
	Path         string
	RecordsField string
	CursorField  string
}

var datasetCatalog = map[string]datasetDefinition{
	"audit": {
		Name: "audit", TagKind: "audit", Method: "POST", Path: "/audit",
	},
	"credentials": n8nList("credentials", "creds"),
	"executions":  n8nList("executions", "executions"),
	"projects":    n8nList("projects", "projects"),
	"tags":        n8nList("tags", "tags"),
	"users":       n8nList("users", "users"),
	"variables":   n8nList("variables", "variables"),
	"workflows":   n8nList("workflows", "workflows"),
}

func n8nList(name, tagKind string) datasetDefinition {
	return datasetDefinition{
		Name: name, TagKind: tagKind, Method: "GET", Path: "/" + name,
		RecordsField: "data", CursorField: "nextCursor",
	}
}

func resolveDatasets(selected []string) ([]datasetDefinition, error) {
	if len(selected) == 0 {
		selected = make([]string, 0, len(datasetCatalog))
		for name := range datasetCatalog {
			selected = append(selected, name)
		}
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]datasetDefinition, 0, len(selected))
	for _, raw := range selected {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("Dataset must not be empty")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("Dataset %q is configured more than once", name)
		}
		definition, ok := datasetCatalog[name]
		if !ok {
			return nil, fmt.Errorf("unsupported Dataset %q", name)
		}
		seen[name] = struct{}{}
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
