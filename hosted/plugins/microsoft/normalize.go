package microsoft

// Field aliases are additive. The original record remains in record, including
// any vendor-owned canonical names. Rules never treat a target account as the
// actor of an audit event. Only documented selector-specific paths are used.
type normalizationRule struct {
	target string
	paths  []string
}

type normalizationCollision struct {
	Field            string   `json:"field"`
	SelectedPath     string   `json:"selectedPath"`
	ConflictingPaths []string `json:"conflictingPaths"`
}

func canonicalFields(record map[string]any, dataset Dataset) (map[string]string, []normalizationCollision) {
	rules := []normalizationRule{
		{target: "recordId", paths: []string{"recordId", dataset.IDField}},
		{target: "recordTimestamp", paths: []string{"recordTimestamp", dataset.TimeField}},
	}
	switch dataset.Name {
	case "entra-signins":
		rules = append(rules,
			normalizationRule{"userName", []string{"userName", "userPrincipalName"}},
			normalizationRule{"sourceIp", []string{"sourceIp", "ipAddress"}})
	case "entra-directory-audits":
		rules = append(rules,
			normalizationRule{"userName", []string{"userName", "initiatedBy.user.userPrincipalName"}},
			normalizationRule{"actionName", []string{"actionName", "activityDisplayName"}})
	}
	fields := make(map[string]string)
	collisions := make([]normalizationCollision, 0)
	for _, rule := range rules {
		selectedPath, selectedValue := "", ""
		conflicting := make([]string, 0)
		seen := make(map[string]bool)
		for _, path := range rule.paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			value := stringValueAtPath(record, path)
			if value == "" {
				continue
			}
			if selectedPath == "" {
				selectedPath, selectedValue = path, value
			} else if value != selectedValue {
				conflicting = append(conflicting, path)
			}
		}
		if selectedPath != "" {
			fields[rule.target] = selectedValue
		}
		if len(conflicting) != 0 {
			collisions = append(collisions, normalizationCollision{rule.target, selectedPath, conflicting})
		}
	}
	return fields, collisions
}
