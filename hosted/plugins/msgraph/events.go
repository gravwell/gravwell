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
	"time"
)

// ContentType represents the type of content to poll from the MS Graph Security API.
type ContentType string

const (
	ContentAlerts          ContentType = "alerts"
	ContentSecureScores    ContentType = "secureScores"
	ContentControlProfiles ContentType = "controlProfiles"
)

func (ct ContentType) Tag(tagName, prefix string) string {
	if tagName != "" {
		return tagName
	}
	if prefix != "" {
		return prefix + "-" + string(ct)
	}
	return "msgraph-" + string(ct)
}

// AlertTimestamp extracts createdDateTime from a v2 alert JSON blob.
type AlertTimestamp struct {
	CreatedDateTime time.Time `json:"createdDateTime"`
}

// SecureScoreTimestamp extracts createdDateTime from a secure score JSON blob.
type SecureScoreTimestamp struct {
	CreatedDateTime time.Time `json:"createdDateTime"`
}

// ResourceID extracts the id field from any Graph API resource.
type ResourceID struct {
	ID string `json:"id"`
}

// ODataResponse is the generic paginated response from MS Graph.
type ODataResponse struct {
	Value    []json.RawMessage `json:"value"`
	NextLink string            `json:"@odata.nextLink"`
}

// AuthToken represents the OAuth2 token response.
type AuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	expiresAt   time.Time
}

// AuthErrorResponse is the error returned by the token endpoint.
type AuthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// GraphErrorResponse wraps the error body from Graph API calls.
type GraphErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
