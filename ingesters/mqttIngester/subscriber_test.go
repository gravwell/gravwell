/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"testing"

	"github.com/eclipse/paho.golang/paho"
	"github.com/stretchr/testify/require"
)

// paho collapses every per-topic SUBACK failure into one generic error, so the
// reason codes are the only way to tell a denied ACL from a busy broker.
func TestInspectSuback(t *testing.T) {
	topics := []string{`a/#`, `b/#`, `c/#`}

	tests := []struct {
		name      string
		sa        *paho.Suback
		granted   int
		rejected  int
		permanent bool
		contains  string
	}{
		{
			name:    `nil suback`,
			sa:      nil,
			granted: 0,
		},
		{
			name:    `all granted`,
			sa:      &paho.Suback{Reasons: []byte{0x00, 0x01, 0x02}},
			granted: 3,
		},
		{
			name:      `one denied, rest granted`,
			sa:        &paho.Suback{Reasons: []byte{0x00, 0x87, 0x01}},
			granted:   2,
			rejected:  1,
			permanent: true,
			contains:  `b/#(0x87 not authorized)`,
		},
		{
			name:      `all denied permanently`,
			sa:        &paho.Suback{Reasons: []byte{0x87, 0x8f, 0xa2}},
			rejected:  3,
			permanent: true,
		},
		{
			name:      `quota exceeded is transient`,
			sa:        &paho.Suback{Reasons: []byte{0x97}},
			rejected:  1,
			permanent: false,
		},
		{
			name:      `mixed transient and permanent is not permanent`,
			sa:        &paho.Suback{Reasons: []byte{0x87, 0x97}},
			rejected:  2,
			permanent: false,
		},
		{
			name:      `more reasons than topics does not panic`,
			sa:        &paho.Suback{Reasons: []byte{0x87, 0x87, 0x87, 0x87}},
			rejected:  4,
			permanent: true,
			contains:  `?(0x87 not authorized)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			granted, rejected, permanent := inspectSuback(tt.sa, topics)
			require.Equal(t, tt.granted, granted)
			require.Len(t, rejected, tt.rejected)
			require.Equal(t, tt.permanent, permanent)
			if tt.contains != `` {
				require.Contains(t, rejected, tt.contains)
			}
		})
	}
}

func TestSubackReasonCoversKnownCodes(t *testing.T) {
	// a code we do not know must still render, not return an empty string
	require.Equal(t, `unknown`, subackReason(0xFF))
	require.Equal(t, `not authorized`, subackReason(0x87))
	require.True(t, permanentSubackCode(0x87))
	require.False(t, permanentSubackCode(0x97), "quota exceeded can clear on its own")
	require.False(t, permanentSubackCode(0x00))
}

// A new connection must supersede the previous subscribe attempt, otherwise a
// flapping broker accumulates goroutines that wake to issue stale SUBSCRIBEs.
func TestRestartSubscribeCancelsPrevious(t *testing.T) {
	h := newTestHarness(t, nil)

	first := h.sub.restartSubscribe()
	require.NoError(t, first.Err())

	second := h.sub.restartSubscribe()
	require.ErrorIs(t, first.Err(), context.Canceled, "the previous attempt must be cancelled")
	require.NoError(t, second.Err())

	// shutting the subscriber down cancels the live one too
	h.sub.cf()
	require.ErrorIs(t, second.Err(), context.Canceled)
}
