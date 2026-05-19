/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package msgraph

import (
	"encoding/json"
	"net/url"
	"time"
)

const ODataTimeFormat = "2006-01-02T15:04:05Z"

// ContentTypeEndpoint returns the Graph API path for a given ContentType.
func ContentTypeEndpoint(ct ContentType) string {
	switch ct {
	case ContentAlerts:
		return AlertsEndpoint
	case ContentSecureScores:
		return SecureScoresEndpoint
	case ContentControlProfiles:
		return ControlProfilesEndpoint
	default:
		return ""
	}
}

// BuildParams constructs the OData query parameters for a content type poll.
func BuildParams(ct ContentType, since time.Time) url.Values {
	params := url.Values{}
	params.Set("$top", "100")
	params.Set("$orderby", "createdDateTime asc")
	if ct != ContentControlProfiles {
		filter := "createdDateTime gt " + since.UTC().Format(ODataTimeFormat)
		params.Set("$filter", filter)
	}
	return params
}

// ExtractTimestamp pulls createdDateTime from a raw Graph API JSON response item.
// Falls back to time.Now() for content types without timestamp or when unmarshalling fails.
func ExtractTimestamp(ct ContentType, raw json.RawMessage) time.Time {
	switch ct {
	case ContentAlerts:
		var ts AlertTimestamp
		if err := json.Unmarshal(raw, &ts); err == nil && !ts.CreatedDateTime.IsZero() {
			return ts.CreatedDateTime
		}
	case ContentSecureScores:
		var ts SecureScoreTimestamp
		if err := json.Unmarshal(raw, &ts); err == nil && !ts.CreatedDateTime.IsZero() {
			return ts.CreatedDateTime
		}
	}
	return time.Now()
}

// ExtractID pulls the id field from a raw Graph API JSON response item.
// Falls back to empty string if unmarshalling fails.
func ExtractID(raw json.RawMessage) string {
	var rID ResourceID
	if err := json.Unmarshal(raw, &rID); err == nil {
		return rID.ID
	}
	return ""
}
