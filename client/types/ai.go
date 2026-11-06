/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "time"

// AIHealthcheck - Describes the status of AI features for the current user
type AIHealthcheck struct {
	// Bool indicating if this license/endpoint has unlimited access to remote AI workers
	UnlimitedActions bool

	// The total number of actions allowed for the account in the given time frame
	InitialActions int

	// Count of AI actions remaining for the current user
	RemainingActions int

	// Describes the next moment when the current user is allowed to perform more AI actions
	NextActionRegenDatetime time.Time

	// A soft limit of the number of tokens that can be provided in a chat completion.
	// This value is used to show warnings in the Gravwell UI when a request's prompt token count reaches this number.
	WarnTokens int

	// A hard limit of the number of tokens that can be provided in a chat completion
	MaxTokens int
}
