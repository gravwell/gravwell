package types

import (
	"encoding/json"
	"time"
)

// SavedQuery is a stored Gravwell query. This replaces SearchLibrary in the old types.
type SavedQuery struct {
	CommonFields

	Query              string
	SuggestedTimeframe SavedQueryTimeframe
}

type SavedQueryTimeframe struct {
	Duration  string    `json:"durationString"`
	End       time.Time `json:"end"`
	Start     time.Time `json:"start"`
	Timeframe string    `json:"timeframe"`
	Timezone  string    `json:"timezone"`
}

type SavedQueryListResponse struct {
	BaseListResponse
	Results []SavedQuery `json:"results"`
}

func (sq *SavedQuery) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name        string
		Description string
		Query       string
	}{
		Name:        sq.Name,
		Description: sq.Description,
		Query:       sq.Query,
	})
	return json.RawMessage(b), err
}
