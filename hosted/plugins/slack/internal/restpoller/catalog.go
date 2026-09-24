package restpoller

import (
	"fmt"
	"sort"
	"strings"
)

type paginationKind string

const (
	paginationNone   paginationKind = "none"
	paginationLink   paginationKind = "link"
	paginationCursor paginationKind = "cursor"
	paginationOffset paginationKind = "offset"
)

type authKind string

const (
	authNone      authKind = "none"
	authBearer    authKind = "bearer"
	authBasicUser authKind = "basic-user"
	authHeader    authKind = "header"
)

type Definition struct {
	Product          string
	Name             string
	TagKind          string
	Method           string
	Path             string
	RecordsField     string
	Pagination       paginationKind
	CursorQuery      string
	CursorField      string
	PersistCursor    bool
	Auth             authKind
	AuthHeader       string
	AuthPrefix       string
	PageSizeQuery    string
	MaximumPageSize  int
	OffsetQuery      string
	HasMoreField     string
	RequestBody      string
	RequiredStart    bool
	StartQuery       string
	DocumentationURL string
}

var catalog = map[string]map[string]Definition{
	"atlantis": {
		"locks": {
			Product: "atlantis", Name: "locks", TagKind: "locks", Method: "GET", Path: "/api/locks",
			Auth: authNone, DocumentationURL: "https://www.runatlantis.io/docs/api-endpoints",
		},
		"drift-status": {
			Product: "atlantis", Name: "drift-status", TagKind: "drift-status", Method: "GET", Path: "/api/drift/status",
			Auth: authHeader, AuthHeader: "X-Atlantis-Token", DocumentationURL: "https://www.runatlantis.io/docs/api-endpoints",
		},
	},
	"chatgpt": {
		"audit-logs": {
			Product: "chatgpt", Name: "audit-logs", TagKind: "audit-logs", Method: "GET", Path: "/organization/audit_logs",
			RecordsField: "data", Pagination: paginationCursor, CursorQuery: "after", CursorField: "last_id", PersistCursor: true,
			Auth: authBearer, PageSizeQuery: "limit", MaximumPageSize: 100,
			DocumentationURL: "https://developers.openai.com/api/reference/resources/audit-logs/methods/list",
		},
		"users": {
			Product: "chatgpt", Name: "users", TagKind: "users", Method: "GET", Path: "/organization/users",
			RecordsField: "data", Pagination: paginationCursor, CursorQuery: "after", CursorField: "last_id",
			Auth: authBearer, PageSizeQuery: "limit", MaximumPageSize: 100,
			DocumentationURL: "https://developers.openai.com/api/reference/resources/users/methods/list",
		},
		"groups": {
			Product: "chatgpt", Name: "groups", TagKind: "groups", Method: "GET", Path: "/organization/groups",
			RecordsField: "data", Pagination: paginationCursor, CursorQuery: "after", CursorField: "last_id",
			Auth: authBearer, PageSizeQuery: "limit", MaximumPageSize: 100,
			DocumentationURL: "https://developers.openai.com/api/reference/resources/groups/methods/list",
		},
	},
	"claude": {
		"usage-messages": {
			Product: "claude", Name: "usage-messages", TagKind: "usage-messages", Method: "GET", Path: "/v1/organizations/usage_report/messages",
			RecordsField: "data", Pagination: paginationCursor, CursorQuery: "page", CursorField: "next_page",
			Auth: authHeader, AuthHeader: "x-api-key", PageSizeQuery: "limit", MaximumPageSize: 1000,
			RequiredStart: true, StartQuery: "starting_at",
			DocumentationURL: "https://docs.anthropic.com/en/api/admin-api/usage-cost/get-messages-usage-report",
		},
		"organization": {
			Product: "claude", Name: "organization", TagKind: "organization", Method: "GET", Path: "/v1/organizations/me",
			Auth: authHeader, AuthHeader: "x-api-key",
			DocumentationURL: "https://docs.anthropic.com/en/api/admin-api/organization/get-me",
		},
	},
	"freshworks": {
		"tickets": {
			Product: "freshworks", Name: "tickets", TagKind: "tickets", Method: "GET", Path: "/api/v2/tickets",
			Pagination: paginationLink, Auth: authBasicUser, PageSizeQuery: "per_page", MaximumPageSize: 100,
			StartQuery: "updated_since", DocumentationURL: "https://developers.freshdesk.com/api/#list_all_tickets",
		},
		"agents": {
			Product: "freshworks", Name: "agents", TagKind: "agents", Method: "GET", Path: "/api/v2/agents",
			Pagination: paginationLink, Auth: authBasicUser, PageSizeQuery: "per_page", MaximumPageSize: 100,
			DocumentationURL: "https://developers.freshdesk.com/api/#list_all_agents",
		},
		"contacts": {
			Product: "freshworks", Name: "contacts", TagKind: "contacts", Method: "GET", Path: "/api/v2/contacts",
			Pagination: paginationLink, Auth: authBasicUser, PageSizeQuery: "per_page", MaximumPageSize: 100,
			DocumentationURL: "https://developers.freshdesk.com/api/#list_all_contacts",
		},
	},
	"n8n": {
		"users":       n8nList("users", "/users"),
		"executions":  n8nList("executions", "/executions"),
		"workflows":   n8nList("workflows", "/workflows"),
		"credentials": n8nList("creds", "/credentials"),
		"tags":        n8nList("tags", "/tags"),
		"variables":   n8nList("variables", "/variables"),
		"projects":    n8nList("projects", "/projects"),
		"audit": {
			Product: "n8n", Name: "audit", TagKind: "audit", Method: "POST", Path: "/audit", Auth: authHeader,
			AuthHeader: "X-N8N-API-KEY", RequestBody: "{}", DocumentationURL: "https://docs.n8n.io/connect/n8n-api/audit",
		},
	},
	"netsuite": {
		"suiteql": {
			Product: "netsuite", Name: "suiteql", TagKind: "suiteql", Method: "POST", Path: "/services/rest/query/v1/suiteql",
			RecordsField: "items", Pagination: paginationOffset, OffsetQuery: "offset", HasMoreField: "hasMore",
			Auth: authBearer, PageSizeQuery: "limit", MaximumPageSize: 1000, RequestBody: "suiteql-file",
			DocumentationURL: "https://docs.oracle.com/en/cloud/saas/netsuite/ns-online-help/section_157909186990.html",
		},
	},
	"slack": {
		"audit-logs": {
			Product: "slack", Name: "audit-logs", TagKind: "audit", Method: "GET", Path: "/audit/v1/logs",
			RecordsField: "entries", Pagination: paginationCursor, CursorQuery: "cursor", CursorField: "response_metadata.next_cursor",
			Auth: authBearer, PageSizeQuery: "limit", MaximumPageSize: 9999,
			DocumentationURL: "https://docs.slack.dev/admins/audit-logs-api/",
		},
	},
}

func n8nList(tagKind, path string) Definition {
	return Definition{
		Product: "n8n", Name: strings.TrimPrefix(path, "/"), TagKind: tagKind, Method: "GET", Path: path,
		RecordsField: "data", Pagination: paginationCursor, CursorQuery: "cursor", CursorField: "nextCursor",
		Auth: authHeader, AuthHeader: "X-N8N-API-KEY", PageSizeQuery: "limit", MaximumPageSize: 250,
		DocumentationURL: "https://docs.n8n.io/connect/n8n-api/api-reference",
	}
}

func ResolveDefinitions(product string, selected []string) ([]Definition, error) {
	product = strings.ToLower(strings.TrimSpace(product))
	available, ok := catalog[product]
	if !ok {
		return nil, fmt.Errorf("unsupported Product %q", product)
	}
	if len(selected) == 0 {
		selected = make([]string, 0, len(available))
		for name := range available {
			selected = append(selected, name)
		}
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]Definition, 0, len(selected))
	for _, raw := range selected {
		name := strings.ToLower(strings.TrimSpace(raw))
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("Dataset %q is configured more than once", name)
		}
		definition, exists := available[name]
		if !exists {
			return nil, fmt.Errorf("unsupported Dataset %q for Product %q", name, product)
		}
		seen[name] = struct{}{}
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func Products() []string {
	products := make([]string, 0, len(catalog))
	for product := range catalog {
		products = append(products, product)
	}
	sort.Strings(products)
	return products
}
