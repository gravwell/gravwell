/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// formatRecord enforces compact one-event-per-entry JSON framing while
// preserving the complete selected Microsoft object.
func formatRecord(raw []byte) ([]byte, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("compact selected Microsoft record: %w", err)
	}
	payload := append([]byte(nil), compact.Bytes()...)
	if bytes.ContainsAny(payload, "\r\n") {
		return nil, fmt.Errorf("compacted Microsoft record contains a physical line break")
	}
	return payload, nil
}

func attachSourceIdentity(item *entry.Entry, dataset Dataset) error {
	if strings.TrimSpace(dataset.Name) == "" {
		return fmt.Errorf("required Microsoft source identity is empty")
	}
	if err := item.AddEnumeratedValueEx("_source", dataset.Name); err != nil {
		return fmt.Errorf("attach Microsoft source identity: %w", err)
	}
	return nil
}
