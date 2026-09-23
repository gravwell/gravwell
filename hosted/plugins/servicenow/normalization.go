package servicenow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// PreparedRecord is one compact ServiceNow record plus optional intrinsic
// values produced by the versioned normalization contract.
type PreparedRecord struct {
	Data      []byte
	Intrinsic map[string]string
}

type normalizationRule struct {
	Group   string
	Target  string
	Sources []string
}

var endpointNormalizationFields = map[string]string{
	"aggregate-incidents":     "stats.count",
	"attachment-metadata":     "sys_id,file_name,content_type,size_bytes,table_name,table_sys_id,sys_created_on,sys_updated_on",
	"service-catalog-items":   "sys_id,name,short_description,type,content_type,availability",
	"service-catalogs":        "sys_id,title,description,has_items,has_categories",
	"change-models":           "sys_id.value,sys_updated_on.value,name.value,active.value",
	"cmdb-instances":          "sys_id,name,sys_class_name,install_status,operational_status,company,location,assigned_to,managed_by,sys_created_on,sys_updated_on",
	"scripted-rest-services":  "sys_id,sys_created_on,sys_updated_on,name,namespace,service_id,base_uri,active,is_versioned,default_version",
	"scripted-rest-resources": "sys_id,sys_created_on,sys_updated_on,name,http_method,operation_uri,relative_path,active,requires_authentication,requires_acl_authorization",
}

// PrepareRecord preserves native vendor fields and adds only documented
// canonical aliases when normalization is enabled. Provenance remains
// intrinsic metadata and is attached independently by the runtime.
func PrepareRecord(conf *Config, d Dataset, raw []byte) (PreparedRecord, error) {
	result := PreparedRecord{Data: append([]byte(nil), raw...)}
	enabled, err := conf.NormalizationEnabled()
	if err != nil {
		return PreparedRecord{}, err
	}
	if !enabled {
		return result, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return PreparedRecord{}, fmt.Errorf("decode ServiceNow record for normalization: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(compact.Bytes(), &object); err != nil {
		return PreparedRecord{}, fmt.Errorf("decode ServiceNow record for normalization: %w", err)
	}
	if object == nil {
		return PreparedRecord{}, errors.New("ServiceNow normalization requires a JSON object record")
	}
	rules := conf.normalizationRules
	if rules == nil {
		rules, err = compileNormalizationRules(conf.Normalization_Field)
		if err != nil {
			return PreparedRecord{}, err
		}
	}
	aliases, collisions := applyNormalizationRules(object, normalizationRulesForDataset(d, rules))
	if len(collisions) != 0 {
		result.Intrinsic = map[string]string{"_normalizationCollision": strings.Join(collisions, ",")}
	}
	result.Data, err = appendNormalizationAliases(compact.Bytes(), aliases)
	if err != nil {
		return PreparedRecord{}, fmt.Errorf("encode normalized ServiceNow record: %w", err)
	}
	return result, nil
}

// AttachNormalizationIntrinsic attaches only the finite intrinsic values owned
// by PrepareRecord. It never copies vendor values into an error or log.
func AttachNormalizationIntrinsic(ent *entry.Entry, values map[string]string) error {
	for name, value := range values {
		if name != "_normalizationCollision" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("unsupported ServiceNow normalization intrinsic %q", name)
		}
		if err := ent.AddEnumeratedValueEx(name, []byte(value)); err != nil {
			return fmt.Errorf("attach normalization intrinsic %s: %w", name, err)
		}
	}
	return nil
}

func compileNormalizationRules(values []string) (map[string][]normalizationRule, error) {
	byKey := map[string]normalizationRule{}
	for _, raw := range values {
		left, right, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok || strings.TrimSpace(right) == "" {
			return nil, fmt.Errorf("Normalization-Field %q must use group:target=source1|source2", raw)
		}
		group, target, ok := strings.Cut(strings.TrimSpace(left), ":")
		group = strings.ToLower(strings.TrimSpace(group))
		target = strings.TrimSpace(target)
		if !ok || (group != "all" && !validDatasetName(group)) || !validCanonicalTarget(target) {
			return nil, fmt.Errorf("Normalization-Field %q must use a safe group and lower-camel target", raw)
		}
		sources := []string{}
		seen := map[string]bool{}
		for _, rawSource := range strings.Split(right, "|") {
			source := strings.TrimSpace(rawSource)
			if !validNormalizationSource(source) {
				return nil, fmt.Errorf("Normalization-Field %q has invalid source path %q", raw, source)
			}
			if !seen[source] {
				sources = append(sources, source)
				seen[source] = true
			}
		}
		if !seen[target] {
			sources = append([]string{target}, sources...)
		}
		byKey[group+":"+target] = normalizationRule{Group: group, Target: target, Sources: sources}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := map[string][]normalizationRule{}
	for _, key := range keys {
		rule := byKey[key]
		result[rule.Group] = append(result[rule.Group], rule)
	}
	return result, nil
}

func normalizationRulesForDataset(d Dataset, custom map[string][]normalizationRule) []normalizationRule {
	byTarget := map[string]normalizationRule{}
	for _, rule := range documentedNormalizationRules(d) {
		byTarget[rule.Target] = rule
	}
	groups := []string{"all", strings.TrimPrefix(strings.ToLower(d.Tag), "servicenow-"), strings.ToLower(d.Name)}
	for _, group := range groups {
		for _, rule := range custom[group] {
			byTarget[rule.Target] = rule
		}
	}
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	result := make([]normalizationRule, 0, len(targets))
	for _, target := range targets {
		result = append(result, byTarget[target])
	}
	return result
}

func documentedNormalizationRules(d Dataset) []normalizationRule {
	fields := d.Fields
	if replacement := endpointNormalizationFields[d.Name]; replacement != "" {
		fields = replacement
	} else if fields == "" && d.REST != nil {
		fields = d.REST.Parameters["sysparm_fields"]
	}
	byTarget := map[string]normalizationRule{}
	for _, rawField := range strings.Split(fields, ",") {
		source := strings.TrimSpace(rawField)
		if source == "" || !validNormalizationSource(source) {
			continue
		}
		base := source
		if strings.HasSuffix(base, ".value") {
			base = strings.TrimSuffix(base, ".value")
		} else if index := strings.LastIndex(base, "."); index >= 0 {
			base = base[index+1:]
		}
		target := lowerCamelField(base)
		if !validCanonicalTarget(target) || (target == source && !strings.Contains(source, ".")) {
			continue
		}
		sources := []string{target}
		if base != target {
			sources = append(sources, base)
		}
		if source != base {
			sources = append(sources, source)
		}
		byTarget[target] = normalizationRule{Group: strings.ToLower(d.Name), Target: target, Sources: compactStrings(sources)}
	}
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	result := make([]normalizationRule, 0, len(targets))
	for _, target := range targets {
		result = append(result, byTarget[target])
	}
	return result
}

func applyNormalizationRules(object map[string]json.RawMessage, rules []normalizationRule) (map[string]json.RawMessage, []string) {
	aliases := map[string]json.RawMessage{}
	collisions := []string{}
	for _, rule := range rules {
		var chosen json.RawMessage
		chosenComparable := ""
		chosenSource := ""
		for _, source := range rule.Sources {
			value, ok := normalizationSourceValue(object, source)
			if !ok {
				continue
			}
			comparable := normalizationComparable(value)
			if comparable == "" || comparable == "null" {
				continue
			}
			if chosenComparable == "" {
				chosen, chosenComparable, chosenSource = value, comparable, source
				continue
			}
			if comparable != chosenComparable {
				collisions = append(collisions, rule.Target+"("+chosenSource+"|"+source+")")
			}
		}
		if chosenComparable == "" {
			continue
		}
		if existing, ok := object[rule.Target]; !ok || normalizationComparable(existing) == "" || normalizationComparable(existing) == "null" {
			aliases[rule.Target] = append(json.RawMessage(nil), chosen...)
		}
	}
	sort.Strings(collisions)
	return aliases, compactStrings(collisions)
}

func normalizationSourceValue(object map[string]json.RawMessage, source string) (json.RawMessage, bool) {
	current := object
	var value json.RawMessage
	parts := strings.Split(source, ".")
	for index, part := range parts {
		var ok bool
		value, ok = current[part]
		if !ok {
			return nil, false
		}
		if index < len(parts)-1 {
			var next map[string]json.RawMessage
			if err := json.Unmarshal(value, &next); err != nil {
				return nil, false
			}
			current = next
		}
	}
	return value, true
}

func normalizationComparable(value json.RawMessage) string {
	trimmed := bytes.TrimSpace(value)
	var text string
	if len(trimmed) > 0 && trimmed[0] == '"' && json.Unmarshal(trimmed, &text) == nil {
		return strings.TrimSpace(text)
	}
	var compact bytes.Buffer
	if json.Compact(&compact, trimmed) != nil {
		return ""
	}
	return compact.String()
}

func appendNormalizationAliases(object []byte, aliases map[string]json.RawMessage) ([]byte, error) {
	if len(object) < 2 || object[0] != '{' || object[len(object)-1] != '}' {
		return nil, errors.New("ServiceNow normalization requires a JSON object record")
	}
	targets := make([]string, 0, len(aliases))
	for target := range aliases {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	result := []byte{'{'}
	for _, target := range targets {
		if len(result) > 1 {
			result = append(result, ',')
		}
		key, _ := json.Marshal(target)
		result = append(result, key...)
		result = append(result, ':')
		var compact bytes.Buffer
		if err := json.Compact(&compact, aliases[target]); err != nil {
			return nil, fmt.Errorf("encode normalized ServiceNow alias %s: %w", target, err)
		}
		result = append(result, compact.Bytes()...)
	}
	if len(object) > 2 {
		if len(result) > 1 {
			result = append(result, ',')
		}
		result = append(result, object[1:len(object)-1]...)
	}
	return append(result, '}'), nil
}

func lowerCamelField(value string) string {
	parts := strings.Split(value, "_")
	if len(parts) == 1 {
		return value
	}
	result := strings.ToLower(parts[0])
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return result
}

func validCanonicalTarget(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validNormalizationSource(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}

func normalizationNamespace(values []string) string {
	if len(values) == 0 {
		return "servicenow/normalized-v3"
	}
	canonical := append([]string(nil), values...)
	sort.Strings(canonical)
	digest := sha256.Sum256([]byte(strings.Join(canonical, "\n")))
	return "servicenow/normalized-v3/" + hex.EncodeToString(digest[:6])
}
