/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package skills

import (
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
)

// getCommonField returns the value of one of the edit action's fields.
func getCommonField(cf types.CommonFields, fieldKey string) (string, error) {
	switch fieldKey {
	case "name":
		return cf.Name, nil
	case "desc":
		return cf.Description, nil
	case "labels":
		return strings.Join(cf.Labels, ","), nil
	}
	return "", scaffoldedit.ErrUnknownField(fieldKey)
}

// setCommonField sets one of the edit action's fields. The webserver refuses
// names with leading or trailing whitespace, so the name is trimmed; labels
// are split on commas, dropping empty ones.
func setCommonField(cf *types.CommonFields, fieldKey, val string) (invalid string, err error) {
	switch fieldKey {
	case "name":
		if val = strings.TrimSpace(val); val == "" {
			return "name may not be empty", nil
		}
		cf.Name = val
	case "desc":
		cf.Description = val
	case "labels":
		cf.Labels = []string{}
		for lbl := range strings.SplitSeq(val, ",") {
			if lbl = strings.TrimSpace(lbl); lbl != "" {
				cf.Labels = append(cf.Labels, lbl)
			}
		}
	default:
		return "", scaffoldedit.ErrUnknownField(fieldKey)
	}
	return "", nil
}

// metadataPatch is the patch that sets the fields the edit action manages,
// leaving the content and sharing as they are on the server.
func metadataPatch(cf types.CommonFields) types.CommonFieldsPatch {
	return types.CommonFieldsPatch{
		Name:        types.NewOptional(cf.Name),
		Description: types.NewOptional(cf.Description),
		Labels:      types.NewOptional(cf.Labels),
	}
}
