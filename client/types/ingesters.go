/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"github.com/google/uuid"
)

// ForgetIngesterStatus is the outcome of forgetting a single ingester
type ForgetIngesterStatus string

const (
	ForgetIngesterSuccess ForgetIngesterStatus = `success`
	ForgetIngesterError   ForgetIngesterStatus = `error`
)

// ForgetIngesterResult is the outcome of forgetting a single ingester UUID.
// Indexers track and report ingester UUIDs in canonical lowercase form regardless of how the
// ingester was configured, so UUID is always lowercase when encoded.
type ForgetIngesterResult struct {
	UUID   uuid.UUID
	Status ForgetIngesterStatus
	Error  string `json:",omitempty"` // only set when Status is error
}

// ForgetIngestersResponse is the response to DELETE /api/ingesters/tracking
type ForgetIngestersResponse struct {
	Results       []ForgetIngesterResult
	IndexerErrors map[string]string `json:",omitempty"` // indexers that failed to service the request, keyed by indexer name
}
