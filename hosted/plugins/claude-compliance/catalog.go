// Package claudecompliance collects Claude Enterprise Compliance JSON records.
package claudecompliance

import (
	"encoding/json"
	"strings"
	"time"
)

type Dataset struct {
	Name, Path, Rows, Cursor, Tag, Scope, Window string
	Limit                                        int
	// Time names the record field holding the vendor's event or change time.
	// Empty means the record is a snapshot without one, such as a user whose
	// only timestamp is account creation; those entries use collection time.
	Time string
	// Identity lists the fields that identify one record across polls.
	// Alternatives are separated by "|" and composite keys joined by "+";
	// every field of an alternative must be a non-empty string. A record
	// matching no alternative is identified by its content digest.
	Identity string
}

// Metadata and message operations share semantic tags. Selector and parent
// identity are intrinsic fields; vendor JSON is preserved without wrappers.
var datasets = []Dataset{
	{"activities", "/activities", "data", "after_id", "activities", "activities", "created_at", 5000, "created_at", "id"},
	{"organizations", "/organizations", "data", "page", "directory", "org_data", "", 1000, "", "uuid"},
	{"organization-users", "/organizations/{organization_id}/users", "data", "page", "directory", "user_data", "", 1000, "", "id"},
	{"organization-roles", "/organizations/{organization_id}/roles", "data", "page", "directory", "org_data", "", 1000, "updated_at", "id"},
	{"organization-role", "/organizations/{organization_id}/roles/{role_id}", "", "", "directory", "org_data", "", 0, "updated_at", "id"},
	{"role-permissions", "/organizations/{organization_id}/roles/{role_id}/permissions", "data", "page", "directory", "org_data", "", 1000, "", "resource_type+resource_id+action"},
	{"organization-settings", "/organizations/{organization_id}/settings", "", "", "directory", "org_data", "", 0, "", "organization_id"},
	{"groups", "/groups", "data", "page", "directory", "org_data", "", 1000, "updated_at", "id"},
	{"group", "/groups/{group_id}", "", "", "directory", "org_data", "", 0, "updated_at", "id"},
	{"group-members", "/groups/{group_id}/members", "data", "page", "directory", "user_data", "", 1000, "updated_at", "user_id"},
	{"chats", "/apps/chats", "data", "after_id", "conversations", "user_data", "updated_at", 1000, "updated_at", "id"},
	{"chat-messages", "/apps/chats/{chat_id}/messages", "chat_messages", "after_id", "conversations", "user_data", "updated_at", 1000, "created_at", "id"},
	{"file-metadata", "/apps/chats/files/{file_id}", "", "", "conversations", "user_data", "", 0, "created_at", "id"},
	{"generated-file-metadata", "/apps/chats/generated-files/{file_id}", "", "", "conversations", "user_data", "", 0, "created_at", "id"},
	{"projects", "/apps/projects", "data", "page", "projects", "user_data", "updated_at", 100, "updated_at", "id"},
	{"project", "/apps/projects/{project_id}", "", "", "projects", "user_data", "", 0, "updated_at", "id"},
	{"project-attachments", "/apps/projects/{project_id}/attachments", "data", "page", "projects", "user_data", "", 100, "created_at", "id"},
	{"project-collaborators", "/apps/projects/{project_id}/collaborators", "data", "page", "projects", "user_data", "", 100, "", "type+user_id|type+group_id|type+organization_uuid|type+organization_role"},
	{"project-document", "/apps/projects/documents/{document_id}", "", "", "projects", "user_data", "", 0, "created_at", "id"},
	{"project-document-metadata", "/apps/projects/documents/{document_id}/metadata", "", "", "projects", "user_data", "", 0, "created_at", "id"},
	{"artifact-metadata", "/apps/artifacts/{artifact_version_id}", "", "", "artifacts", "user_data", "", 0, "created_at", "version_id"},
	{"local-sessions", "/apps/sessions/local", "data", "page", "sessions", "user_data", "updated_at", 500, "updated_at", "id"},
	{"local-session", "/apps/sessions/local/{session_id}", "", "", "sessions", "user_data", "", 0, "updated_at", "id"},
	{"local-session-messages", "/apps/sessions/local/{session_id}/messages", "data", "page", "sessions", "user_data", "", 1000, "created_at", "id"},
	{"remote-sessions", "/apps/sessions/remote", "data", "page", "sessions", "user_data", "", 500, "updated_at", "id"},
	{"remote-session-messages", "/apps/sessions/remote/{session_id}/messages", "data", "page", "sessions", "user_data", "", 1000, "created_at", "id"},
}

func Catalog() []Dataset { return append([]Dataset(nil), datasets...) }
func lookup(name string) (Dataset, bool) {
	for _, d := range datasets {
		if d.Name == name {
			return d, true
		}
	}
	return Dataset{}, false
}

// identity returns the stable identity of a compact record under spec, or
// the content digest when no alternative is fully present.
func identity(raw []byte, spec, digest string) string {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(raw, &v)
	for _, alternative := range strings.Split(spec, "|") {
		if alternative == "" {
			continue
		}
		parts := []string{}
		for _, field := range strings.Split(alternative, "+") {
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
