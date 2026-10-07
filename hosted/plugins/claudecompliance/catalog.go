// Package claudecompliance collects Claude Enterprise Compliance JSON records.
package claudecompliance

import (
	"encoding/json"
	"strings"
	"time"
)

// Kind separates the two shapes of data the Compliance API exposes. An Event
// happens once and is never revised, so it is identified and timestamped by
// the moment it occurred. A Record describes a thing that exists and can be
// edited -- a user, a group, a project -- so it is re-listed on every scan and
// has to be deduplicated by identity against the digest seen last time.
type Kind int

const (
	Event Kind = iota
	Record
)

type Dataset struct {
	Name, Path, Rows, Cursor, Tag, Scope, Window string
	Kind                                         Kind
	Limit                                        int
	// Time names the record field holding the vendor's event or change time.
	// Empty means the record is a snapshot without one, such as a user whose
	// only timestamp is account creation; those entries use collection time.
	Time string
	// Identity lists the field sets that identify one record across polls.
	// Each inner slice is one composite alternative, tried in order; every
	// field of an alternative must be a non-empty string. A record matching
	// no alternative is identified by its content digest.
	Identity [][]string
}

// Datasets maps a configured Dataset selector to its endpoint contract.
// Metadata and message operations share semantic tags. The _source and
// _parent intrinsic fields retain dataset and parent context; vendor JSON is
// preserved without wrappers.
//
// Window names the field an endpoint accepts as a documented time filter,
// and is empty for an endpoint that accepts none. An inventory with no
// usable filter is collected as a bounded full scan rather than being
// narrowed by a parameter the endpoint would reject: projects, for example,
// documents creation-time filters while the record itself is mutable, so
// filtering on either time would silently drop edited projects.
var Datasets = map[string]Dataset{
	"activities":                {Name: "activities", Path: "/activities", Rows: "data", Cursor: "after_id", Tag: "activities", Scope: "activities", Window: "created_at", Kind: Event, Limit: 5000, Time: "created_at", Identity: [][]string{{"id"}}},
	"organizations":             {Name: "organizations", Path: "/organizations", Rows: "data", Cursor: "page", Tag: "directory", Scope: "org_data", Kind: Record, Limit: 1000, Identity: [][]string{{"uuid"}}},
	"organization-users":        {Name: "organization-users", Path: "/organizations/{organization_id}/users", Rows: "data", Cursor: "page", Tag: "directory", Scope: "user_data", Kind: Record, Limit: 1000, Identity: [][]string{{"id"}}},
	"organization-roles":        {Name: "organization-roles", Path: "/organizations/{organization_id}/roles", Rows: "data", Cursor: "page", Tag: "directory", Scope: "org_data", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"organization-role":         {Name: "organization-role", Path: "/organizations/{organization_id}/roles/{role_id}", Tag: "directory", Scope: "org_data", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"role-permissions":          {Name: "role-permissions", Path: "/organizations/{organization_id}/roles/{role_id}/permissions", Rows: "data", Cursor: "page", Tag: "directory", Scope: "org_data", Kind: Record, Limit: 1000, Identity: [][]string{{"resource_type", "resource_id", "action"}}},
	"organization-settings":     {Name: "organization-settings", Path: "/organizations/{organization_id}/settings", Tag: "directory", Scope: "org_data", Kind: Record, Identity: [][]string{{"organization_id"}}},
	"groups":                    {Name: "groups", Path: "/groups", Rows: "data", Cursor: "page", Tag: "directory", Scope: "org_data", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"group":                     {Name: "group", Path: "/groups/{group_id}", Tag: "directory", Scope: "org_data", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"group-members":             {Name: "group-members", Path: "/groups/{group_id}/members", Rows: "data", Cursor: "page", Tag: "directory", Scope: "user_data", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"user_id"}}},
	"chats":                     {Name: "chats", Path: "/apps/chats", Rows: "data", Cursor: "after_id", Tag: "conversations", Scope: "user_data", Window: "updated_at", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"chat-messages":             {Name: "chat-messages", Path: "/apps/chats/{chat_id}/messages", Rows: "chat_messages", Cursor: "after_id", Tag: "conversations", Scope: "user_data", Window: "updated_at", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}},
	"file-metadata":             {Name: "file-metadata", Path: "/apps/chats/files/{file_id}", Tag: "conversations", Scope: "user_data", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"generated-file-metadata":   {Name: "generated-file-metadata", Path: "/apps/chats/generated-files/{file_id}", Tag: "conversations", Scope: "user_data", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"projects":                  {Name: "projects", Path: "/apps/projects", Rows: "data", Cursor: "page", Tag: "projects", Scope: "user_data", Kind: Record, Limit: 100, Time: "updated_at", Identity: [][]string{{"id"}}},
	"project":                   {Name: "project", Path: "/apps/projects/{project_id}", Tag: "projects", Scope: "user_data", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"project-attachments":       {Name: "project-attachments", Path: "/apps/projects/{project_id}/attachments", Rows: "data", Cursor: "page", Tag: "projects", Scope: "user_data", Kind: Record, Limit: 100, Time: "created_at", Identity: [][]string{{"id"}}},
	"project-collaborators":     {Name: "project-collaborators", Path: "/apps/projects/{project_id}/collaborators", Rows: "data", Cursor: "page", Tag: "projects", Scope: "user_data", Kind: Record, Limit: 100, Identity: [][]string{{"type", "user_id"}, {"type", "group_id"}, {"type", "organization_uuid"}, {"type", "organization_role"}}},
	"project-document":          {Name: "project-document", Path: "/apps/projects/documents/{document_id}", Tag: "projects", Scope: "user_data", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"project-document-metadata": {Name: "project-document-metadata", Path: "/apps/projects/documents/{document_id}/metadata", Tag: "projects", Scope: "user_data", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"artifact-metadata":         {Name: "artifact-metadata", Path: "/apps/artifacts/{artifact_version_id}", Tag: "artifacts", Scope: "user_data", Kind: Record, Time: "created_at", Identity: [][]string{{"version_id"}}},
	"local-sessions":            {Name: "local-sessions", Path: "/apps/sessions/local", Rows: "data", Cursor: "page", Tag: "sessions", Scope: "user_data", Window: "updated_at", Kind: Record, Limit: 500, Time: "updated_at", Identity: [][]string{{"id"}}},
	"local-session":             {Name: "local-session", Path: "/apps/sessions/local/{session_id}", Tag: "sessions", Scope: "user_data", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"local-session-messages":    {Name: "local-session-messages", Path: "/apps/sessions/local/{session_id}/messages", Rows: "data", Cursor: "page", Tag: "sessions", Scope: "user_data", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}},
	"remote-sessions":           {Name: "remote-sessions", Path: "/apps/sessions/remote", Rows: "data", Cursor: "page", Tag: "sessions", Scope: "user_data", Kind: Record, Limit: 500, Time: "updated_at", Identity: [][]string{{"id"}}},
	"remote-session-messages":   {Name: "remote-session-messages", Path: "/apps/sessions/remote/{session_id}/messages", Rows: "data", Cursor: "page", Tag: "sessions", Scope: "user_data", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}},
}

// identity returns the stable identity of a compact record under spec, or
// the content digest when no alternative is fully present.
func identity(raw []byte, spec [][]string, digest string) string {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(raw, &v)
	for _, alternative := range spec {
		parts := make([]string, 0, len(alternative))
		for _, field := range alternative {
			var s string
			if json.Unmarshal(v[field], &s) != nil || s == "" {
				parts = nil
				break
			}
			parts = append(parts, field+":"+s)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\x1f")
		}
	}
	return "sha256:" + digest
}

// sourceTime returns the RFC3339 time in field, or fallback when field is
// empty, absent, null, or lacks an explicit offset.
func sourceTime(raw []byte, field string, fallback time.Time) time.Time {
	if field == "" {
		return fallback
	}
	var v map[string]json.RawMessage
	_ = json.Unmarshal(raw, &v)
	var s string
	if json.Unmarshal(v[field], &s) == nil {
		if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
			return t.UTC()
		}
	}
	return fallback
}
