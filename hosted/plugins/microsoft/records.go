package microsoft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// recordDigest hashes a stable semantic representation of the selected vendor
// record. Object keys are canonicalized by encoding/json. Selector-specific
// Microsoft enrichment known to vary between otherwise identical reads is
// removed only from this change fingerprint. The original compact vendor
// record is still emitted unchanged.
func recordDigest(raw []byte, dataset Dataset) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode record for digest: %w", err)
	}
	applyDigestPolicy(value, dataset.Name)
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode record for digest: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// applyDigestPolicy removes only vendor-generated enrichment known to vary
// between otherwise identical reads. The complete raw record is still emitted
// on the first observation and whenever any retained state field changes.
func applyDigestPolicy(value any, selector string) {
	switch selector {
	case "azure-role-definitions":
		removeDigestPath(value, "properties", "createdOn")
		removeDigestPath(value, "properties", "updatedOn")
	case "defender-cloud-assessments":
		removeDigestPath(value, "properties", "additionalData")
	case "defender-cloud-secure-score-controls":
		// Microsoft returns this expanded definition membership with the
		// control record. Assessment metadata is collected independently.
		removeDigestPath(value, "properties", "definition", "properties", "assessmentDefinitions")
	}
}

func removeDigestPath(value any, path ...string) {
	current, ok := value.(map[string]any)
	if !ok || len(path) == 0 {
		return
	}
	for _, part := range path[:len(path)-1] {
		next, found := current[part].(map[string]any)
		if !found {
			return
		}
		current = next
	}
	delete(current, path[len(path)-1])
}

// formatRecord enforces compact single-entry framing at the final write
// boundary. Enabled mode wraps the complete source record in the versioned
// Microsoft normalization contract and routes it to a distinct tag.
func apiMetadata(dataset Dataset) map[string]string {
	return map[string]string{
		"_vendor":     "Microsoft",
		"_product":    dataset.Product,
		"_source":     dataset.Name,
		"_recordType": dataset.TagGroup,
		"_endpoint":   strings.TrimSpace(dataset.Method() + " " + dataset.Path),
		"_apiVersion": dataset.APIVersion(),
	}
}

func formatRecord(raw []byte, identity string, timestamp time.Time, dataset Dataset, normalization string) ([]byte, map[string]string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, nil, fmt.Errorf("compact selected Microsoft record: %w", err)
	}
	payload := append([]byte(nil), compact.Bytes()...)
	if bytes.ContainsAny(payload, "\r\n") {
		return nil, nil, fmt.Errorf("compacted Microsoft record contains a physical line break")
	}
	metadata := apiMetadata(dataset)
	conf := Config{Normalization: normalization}
	enabled, err := conf.NormalizationEnabled()
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return payload, metadata, nil
	}
	var record map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&record); err != nil {
		return nil, nil, err
	}
	if record == nil {
		return nil, nil, fmt.Errorf("Microsoft normalization requires an object record")
	}
	canonical, collisions := canonicalFields(record, dataset)
	wrapper := map[string]any{
		"contractVersion":           "microsoft-normalization-v1",
		"product":                   dataset.Product,
		"recordType":                dataset.TagGroup,
		"selector":                  dataset.Name,
		"sourceId":                  identity,
		"sourceTimestamp":           timestamp.UTC().Format(time.RFC3339Nano),
		"record":                    record,
		"fieldNormalizationVersion": "microsoft-fields-v1",
		"canonical":                 canonical,
		"normalizationCollisions":   collisions,
	}
	for name, value := range metadata {
		wrapper[name] = value
	}
	normalized, err := json.Marshal(wrapper)
	if err != nil {
		return nil, nil, err
	}
	return normalized, metadata, nil
}

func attachIntrinsic(item *entry.Entry, values map[string]string) error {
	for _, name := range []string{"_vendor", "_product", "_source", "_recordType", "_endpoint", "_apiVersion"} {
		value, ok := values[name]
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("required provenance value %s is empty", name)
		}
		if name == "_endpoint" && strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("provenance endpoint contains a physical line break")
		}
	}
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if err := item.AddEnumeratedValueEx(name, values[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
