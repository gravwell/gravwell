// Package claudecompliance collects Claude Enterprise Compliance JSON records.
package claudecompliance

type Dataset struct {
	Name, Path, Rows, Cursor, Tag, Scope, Window string
	Limit                                        int
}

// Metadata and message operations share semantic tags. Selector and parent
// identity are intrinsic fields; vendor JSON is preserved without wrappers.
var datasets = []Dataset{
	{"activities", "/activities", "data", "after_id", "activities", "activities", "created_at", 5000},
	{"organizations", "/organizations", "data", "page", "directory", "org_data", "", 1000},
	{"organization-users", "/organizations/{organization_id}/users", "data", "page", "directory", "user_data", "", 1000},
	{"organization-roles", "/organizations/{organization_id}/roles", "data", "page", "directory", "org_data", "", 1000},
	{"organization-role", "/organizations/{organization_id}/roles/{role_id}", "", "", "directory", "org_data", "", 0},
	{"role-permissions", "/organizations/{organization_id}/roles/{role_id}/permissions", "data", "page", "directory", "org_data", "", 1000},
	{"organization-settings", "/organizations/{organization_id}/settings", "", "", "directory", "org_data", "", 0},
	{"groups", "/groups", "data", "page", "directory", "org_data", "", 1000},
	{"group", "/groups/{group_id}", "", "", "directory", "org_data", "", 0},
	{"group-members", "/groups/{group_id}/members", "data", "page", "directory", "user_data", "", 1000},
	{"chats", "/apps/chats", "data", "after_id", "conversations", "user_data", "updated_at", 1000},
	{"chat-messages", "/apps/chats/{chat_id}/messages", "chat_messages", "after_id", "conversations", "user_data", "updated_at", 1000},
	{"file-metadata", "/apps/chats/files/{file_id}", "", "", "conversations", "user_data", "", 0},
	{"generated-file-metadata", "/apps/chats/generated-files/{file_id}", "", "", "conversations", "user_data", "", 0},
	{"projects", "/apps/projects", "data", "page", "projects", "user_data", "updated_at", 100},
	{"project", "/apps/projects/{project_id}", "", "", "projects", "user_data", "", 0},
	{"project-attachments", "/apps/projects/{project_id}/attachments", "data", "page", "projects", "user_data", "", 100},
	{"project-collaborators", "/apps/projects/{project_id}/collaborators", "data", "page", "projects", "user_data", "", 100},
	{"project-document", "/apps/projects/documents/{document_id}", "", "", "projects", "user_data", "", 0},
	{"project-document-metadata", "/apps/projects/documents/{document_id}/metadata", "", "", "projects", "user_data", "", 0},
	{"artifact-metadata", "/apps/artifacts/{artifact_version_id}", "", "", "artifacts", "user_data", "", 0},
	{"local-sessions", "/apps/sessions/local", "data", "page", "sessions", "user_data", "updated_at", 500},
	{"local-session", "/apps/sessions/local/{session_id}", "", "", "sessions", "user_data", "", 0},
	{"local-session-messages", "/apps/sessions/local/{session_id}/messages", "data", "page", "sessions", "user_data", "", 1000},
	{"remote-sessions", "/apps/sessions/remote", "data", "page", "sessions", "user_data", "", 500},
	{"remote-session-messages", "/apps/sessions/remote/{session_id}/messages", "data", "page", "sessions", "user_data", "", 1000},
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
