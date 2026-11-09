// Package claudecompliance collects Claude Enterprise Compliance JSON records.
package claudecompliance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	Name, Path, Rows, Cursor, Tag, Window string
	Kind                                  Kind
	Limit                                 int
	// SessionContext permits this operation's documented top-level session
	// envelope to be attached as the bounded _session intrinsic field.
	SessionContext bool
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

const maxRecordFields = 1024

var (
	errRecordFieldLimit = errors.New("Compliance record exceeds field-count limit")
	errInvalidRecord    = errors.New("invalid Compliance record")
	errDuplicateField   = errors.New("Compliance record repeats a selected field")
)

// selectedFields reads only the named top-level fields. Walking the object as
// a stream avoids allocating a map entry for every vendor-controlled key, and
// the field-count ceiling bounds CPU work before any caller examines values.
// A record that supplies a selected field more than once is rejected: keeping
// either copy would make the decision that reads it depend on the decoder
// rather than on the record.
func selectedFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, errInvalidRecord
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("Compliance record is not an object")
	}
	result := make(map[string]json.RawMessage, len(wanted))
	fields := 0
	for dec.More() {
		tok, err = dec.Token()
		if err != nil {
			return nil, errInvalidRecord
		}
		name, ok := tok.(string)
		if !ok {
			return nil, errors.New("invalid Compliance record field")
		}
		fields++
		if fields > maxRecordFields {
			return nil, errRecordFieldLimit
		}
		if _, ok = wanted[name]; ok {
			if _, repeated := result[name]; repeated {
				return nil, fmt.Errorf("%w: %s", errDuplicateField, name)
			}
			var value json.RawMessage
			if err = dec.Decode(&value); err != nil {
				return nil, errInvalidRecord
			}
			result[name] = value
			continue
		}
		if err = skipJSONValue(dec); err != nil {
			return nil, errInvalidRecord
		}
	}
	if tok, err = dec.Token(); err != nil {
		return nil, errInvalidRecord
	}
	if tok != json.Delim('}') {
		return nil, errors.New("invalid Compliance record terminator")
	}
	return result, nil
}

// decisionFields are the top-level record fields whose values decide
// something: whether the parent is deleted, which child it spawns, the
// record's dedup identity, its event timestamp, and the time that orders
// manifest eviction. Duplicates among the names are harmless; the scan builds
// a set.
func decisionFields(d Dataset) []string {
	names := make([]string, 0, 8)
	// "id"/"uuid" carry child-discovery identity, "deleted_at" vendor
	// deletion, and "status" a remote session's readiness.
	names = append(names, "id", "uuid", "deleted_at", "status")
	if d.Time != "" {
		names = append(names, d.Time)
	}
	if d.Window != "" {
		names = append(names, d.Window)
	}
	for _, alternative := range d.Identity {
		names = append(names, alternative...)
	}
	return names
}

// duplicateDecisionField returns the first of names that raw supplies more
// than once at the top level. Work proportional to the record is already
// bounded by the response ceiling, and malformed input is not this function's
// concern: the callers that decode values report that separately.
func duplicateDecisionField(raw []byte, names ...string) (string, bool) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		if name != "" {
			wanted[name] = true
		}
	}
	if len(wanted) == 0 {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", false
	}
	seen := make(map[string]bool, len(wanted))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		name, ok := tok.(string)
		if !ok {
			return "", false
		}
		if wanted[name] {
			if seen[name] {
				return name, true
			}
			seen[name] = true
		}
		if err = skipJSONValue(dec); err != nil {
			return "", false
		}
	}
	return "", false
}

func skipJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return errInvalidRecord
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("invalid Compliance JSON delimiter")
	}
	for depth := 1; depth > 0; {
		tok, err = dec.Token()
		if err != nil {
			return errInvalidRecord
		}
		if delim, ok = tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
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
	"activities":                {Name: "activities", Path: "/activities", Rows: "data", Cursor: "after_id", Tag: "activities", Window: "created_at", Kind: Event, Limit: 5000, Time: "created_at", Identity: [][]string{{"id"}}},
	"organizations":             {Name: "organizations", Path: "/organizations", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Identity: [][]string{{"uuid"}}},
	"organization-users":        {Name: "organization-users", Path: "/organizations/{organization_id}/users", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Identity: [][]string{{"id"}}},
	"organization-roles":        {Name: "organization-roles", Path: "/organizations/{organization_id}/roles", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"organization-role":         {Name: "organization-role", Path: "/organizations/{organization_id}/roles/{role_id}", Tag: "directory", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"role-permissions":          {Name: "role-permissions", Path: "/organizations/{organization_id}/roles/{role_id}/permissions", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Identity: [][]string{{"resource_type", "resource_id", "action"}}},
	"organization-settings":     {Name: "organization-settings", Path: "/organizations/{organization_id}/settings", Tag: "directory", Kind: Record, Identity: [][]string{{"organization_id"}}},
	"groups":                    {Name: "groups", Path: "/groups", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"group":                     {Name: "group", Path: "/groups/{group_id}", Tag: "directory", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"group-members":             {Name: "group-members", Path: "/groups/{group_id}/members", Rows: "data", Cursor: "page", Tag: "directory", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"user_id"}}},
	"chats":                     {Name: "chats", Path: "/apps/chats", Rows: "data", Cursor: "after_id", Tag: "conversations", Window: "updated_at", Kind: Record, Limit: 1000, Time: "updated_at", Identity: [][]string{{"id"}}},
	"chat-messages":             {Name: "chat-messages", Path: "/apps/chats/{chat_id}/messages", Rows: "chat_messages", Cursor: "after_id", Tag: "conversations", Window: "created_at", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}, SessionContext: true},
	"file-metadata":             {Name: "file-metadata", Path: "/apps/chats/files/{file_id}", Tag: "conversations", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"generated-file-metadata":   {Name: "generated-file-metadata", Path: "/apps/chats/generated-files/{file_id}", Tag: "conversations", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"projects":                  {Name: "projects", Path: "/apps/projects", Rows: "data", Cursor: "page", Tag: "projects", Kind: Record, Limit: 100, Time: "updated_at", Identity: [][]string{{"id"}}},
	"project":                   {Name: "project", Path: "/apps/projects/{project_id}", Tag: "projects", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"project-attachments":       {Name: "project-attachments", Path: "/apps/projects/{project_id}/attachments", Rows: "data", Cursor: "page", Tag: "projects", Kind: Record, Limit: 100, Time: "created_at", Identity: [][]string{{"id"}}},
	"project-collaborators":     {Name: "project-collaborators", Path: "/apps/projects/{project_id}/collaborators", Rows: "data", Cursor: "page", Tag: "projects", Kind: Record, Limit: 100, Identity: [][]string{{"type", "user_id"}, {"type", "group_id"}, {"type", "organization_uuid"}, {"type", "organization_role"}}},
	"project-document":          {Name: "project-document", Path: "/apps/projects/documents/{document_id}", Tag: "projects", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"project-document-metadata": {Name: "project-document-metadata", Path: "/apps/projects/documents/{document_id}/metadata", Tag: "projects", Kind: Record, Time: "created_at", Identity: [][]string{{"id"}}},
	"artifact-metadata":         {Name: "artifact-metadata", Path: "/apps/artifacts/{artifact_version_id}", Tag: "artifacts", Kind: Record, Time: "created_at", Identity: [][]string{{"version_id"}}},
	"local-sessions":            {Name: "local-sessions", Path: "/apps/sessions/local", Rows: "data", Cursor: "page", Tag: "sessions", Window: "updated_at", Kind: Record, Limit: 500, Time: "updated_at", Identity: [][]string{{"id"}}},
	"local-session":             {Name: "local-session", Path: "/apps/sessions/local/{session_id}", Tag: "sessions", Kind: Record, Time: "updated_at", Identity: [][]string{{"id"}}},
	"local-session-messages":    {Name: "local-session-messages", Path: "/apps/sessions/local/{session_id}/messages", Rows: "data", Cursor: "page", Tag: "sessions", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}, SessionContext: true},
	"remote-sessions":           {Name: "remote-sessions", Path: "/apps/sessions/remote", Rows: "data", Cursor: "page", Tag: "sessions", Kind: Record, Limit: 500, Time: "updated_at", Identity: [][]string{{"id"}}},
	"remote-session-messages":   {Name: "remote-session-messages", Path: "/apps/sessions/remote/{session_id}/messages", Rows: "data", Cursor: "page", Tag: "sessions", Kind: Event, Limit: 1000, Time: "created_at", Identity: [][]string{{"id"}}, SessionContext: true},
}

// identity returns the stable identity of a compact record under spec, or
// the content digest when no alternative is fully present.
func identity(raw []byte, spec [][]string, digest string) string {
	var names []string
	for _, alternative := range spec {
		names = append(names, alternative...)
	}
	v, err := selectedFields(raw, names...)
	if err != nil {
		return "sha256:" + digest
	}
	for _, alternative := range spec {
		parts := make([]string, 0, len(alternative))
		for _, field := range alternative {
			var s string
			// Keep vendor-selected manifest keys under the same byte ceiling as
			// discovered parameters. The content digest is fixed-size fallback.
			if json.Unmarshal(v[field], &s) != nil || s == "" || len(s) > maxDiscoveredParameterLen {
				parts = nil
				break
			}
			parts = append(parts, field+":"+s)
		}
		if len(parts) > 0 {
			// The vendor-selected identity controls manifest map keys, which are
			// JSON-encoded and persisted on every page. Hash the bounded identity
			// material so escaping-heavy values cannot inflate a nominally bounded
			// manifest into hundreds of megabytes.
			sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
			return "identity-sha256:" + hex.EncodeToString(sum[:])
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
	v, err := selectedFields(raw, field)
	if err != nil {
		return fallback
	}
	var s string
	if json.Unmarshal(v[field], &s) == nil {
		if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
			return t.UTC()
		}
	}
	return fallback
}
