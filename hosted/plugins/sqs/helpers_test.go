package sqs

import (
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func TestExtractTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name             string
		attributes       map[string]string
		ignoreTimestamps bool
		wantNow          bool
		wantTS           time.Time
	}{
		{
			name:       "valid SentTimestamp",
			attributes: map[string]string{"SentTimestamp": strconv.FormatInt(now.UnixMilli(), 10)},
			wantTS:     time.UnixMilli(now.UnixMilli()),
		},
		{
			name:       "missing attribute falls back to now",
			attributes: map[string]string{},
			wantNow:    true,
		},
		{
			name:       "unparseable attribute falls back to now",
			attributes: map[string]string{"SentTimestamp": "not-a-number"},
			wantNow:    true,
		},
		{
			name:             "ignore timestamps always uses now",
			attributes:       map[string]string{"SentTimestamp": strconv.FormatInt(now.UnixMilli(), 10)},
			ignoreTimestamps: true,
			wantNow:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := types.Message{Attributes: tt.attributes}
			got := ExtractTimestamp(m, tt.ignoreTimestamps)
			if tt.wantNow {
				if diff := time.Since(got); diff < 0 || diff > 5*time.Second {
					t.Errorf("ExtractTimestamp() = %v, want within 5s of now", got)
				}
				return
			}
			if !got.Equal(tt.wantTS) {
				t.Errorf("ExtractTimestamp() = %v, want %v", got, tt.wantTS)
			}
		})
	}
}
