/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/stretchr/testify/assert"
)

func newEditAction(opts scaffoldedit.Options) action.Pair {
	return scaffoldedit.NewEditAction("test", "tests",
		scaffoldedit.Config{
			"field": scaffoldedit.FieldName("item"),
		},
		scaffoldedit.SubroutineSet[string, string]{
			SelectSub: func(id string) (item string, err error) {
				return "item", nil
			},
			FetchSub: func() (items []string, err error) {
				return []string{"item1", "item2"}, nil
			},
			GetFieldSub: func(item, fieldKey string) (value string, err error) {
				return "field", nil
			},
			SetFieldSub: func(item *string, fieldKey, val string) (invalid string, err error) {
				return "", nil
			},
			GetTitleSub: func(item string) string {
				return "title"
			},
			GetDescriptionSub: func(item string) string {
				return "desc"
			},
			UpdateSub: func(data *string) (itemTitle string, err error) {
				return "title", nil
			},
		},
		opts,
	)
}

func TestOptions(t *testing.T) {
	t.Run("All options are applied automatically", func(t *testing.T) {
		pair := newEditAction(scaffoldedit.Options{
			CommonOptions: scaffold.CommonOptions{
				Aliases:   []string{"a", "b"},
				AdminOnly: true,
			},
		})
		assert.Equal(t, []string{"e", "a", "b"}, pair.Action.Aliases)
		assert.True(t, annotations.IsAdminOnly(pair.Action))
	})
	t.Run("Options are ignored if not set", func(t *testing.T) {
		pair := newEditAction(scaffoldedit.Options{})
		assert.Equal(t, []string{"e"}, pair.Action.Aliases)
		assert.Len(t, pair.Action.Annotations, 0)
	})
}
