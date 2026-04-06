// Package tokens introduces actions for managing API tokens.
package tokens

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "tokens"
		short string = "manage API tokens"
		long  string = "Tokens are API keys that can be used to authenticate requests to Gravwell." +
			" Each token can be scoped to specific capabilities and optionally given an expiration date."
	)
	return treeutils.GenerateNav(use, short, long, []string{"token"},
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			get(),
			create(),
			delete(),
			regenerate(),
		})
}

func list() action.Pair {
	const (
		short string = "list tokens on the system"
		long  string = "Lists information about the API tokens you have access to."
	)
	return scaffoldlist.NewListAction(short, long,
		types.Token{}, func(fs *pflag.FlagSet) ([]types.Token, error) {
			resp, err := connection.Client.ListTokens(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		scaffoldlist.Options{
			DefaultColumns: []string{
				"ID",
				"Name",
				"Description",
				"ExpiresAt",
			},
		})
}

func get() action.Pair {
	var tokens []types.Token // tokens for the current run; reset by ValidateArgs
	return scaffoldlist.NewListAction(
		"get token details",
		"Retrieves details about specified tokens, based on list of IDs provided as arguments.",
		types.Token{},
		func(fs *pflag.FlagSet) ([]types.Token, error) {

			return tokens, nil
		},
		scaffoldlist.Options{
			Use: "get",
			CmdMods: func(c *cobra.Command) {
				c.Example = "get ID1 ID2"
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				tokens = []types.Token{} // clear cache
				if len(fs.Args()) < 1 {
					return "you must provide at least one token ID", nil
				}
				// fetch and cache the tokens
				for _, id := range fs.Args() {
					t, err := connection.Client.GetToken(id)
					if err != nil {
						if errors.Is(err, client.ErrNotFound) || strings.Contains(err.Error(), "Not Found") {
							return "unknown token ID: " + id, nil
						}
						return "", fmt.Errorf("failed to get token %v: %w", id, err)
					}
					tokens = append(tokens, t)
				}
				return "", nil
			},
		})
}

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name": scaffoldcreate.FieldName("token"),
		"desc": scaffoldcreate.FieldDescription("token"),
		"capabilities": {
			Required:      false,
			Title:         "Capabilities",
			Usage:         "comma-separated list of capabilities to grant the token",
			Type:          scaffoldcreate.Text,
			FlagShorthand: 'c',
			Order:         80,
		},
		"expires": {
			Required:      false,
			Title:         "Expires At",
			Usage:         "expiration date for the token (RFC3339 format, e.g. 2026-01-01T00:00:00Z); leave blank for no expiry",
			Type:          scaffoldcreate.Text,
			FlagShorthand: 'e',
			Order:         60,
		},
	}

	return scaffoldcreate.NewCreateAction("token", fields,
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			tc := types.TokenCreate{
				Name:        fieldValues["name"],
				Description: fieldValues["desc"],
			}

			if caps, found := fieldValues["capabilities"]; found && strings.TrimSpace(caps) != "" {
				raw := strings.Split(strings.TrimSpace(caps), ",")
				tc.Capabilities = make([]string, 0, len(raw))
				for _, c := range raw {
					if trimmed := strings.TrimSpace(c); trimmed != "" {
						tc.Capabilities = append(tc.Capabilities, trimmed)
					}
				}
			}

			if exp, found := fieldValues["expires"]; found && strings.TrimSpace(exp) != "" {
				t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(exp))
				if parseErr != nil {
					return "", "expires must be in RFC3339 format (e.g. 2026-01-01T00:00:00Z)", nil
				}
				tc.ExpiresAt = t
			}

			tf, err := connection.Client.CreateToken(tc)
			if err != nil {
				return "", "", err
			}

			return tf.ID, "", nil
		}, scaffoldcreate.Options{})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("token", "tokens",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetToken(id)
				return err
			}
			return connection.Client.DeleteToken(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			resp, err := connection.Client.ListTokens(nil)
			if err != nil {
				return nil, err
			}
			var items = make([]scaffolddelete.Item[string], len(resp.Results))
			for i, t := range resp.Results {
				items[i] = scaffolddelete.NewItem(t.Name, t.Description, t.ID)
			}
			return items, nil
		})
}

func regenerate() action.Pair {
	return scaffoldedit.NewEditAction("token", "tokens",
		scaffoldedit.Config{
			"expires": {
				Required: false,
				Title:    "Expires At",
				Usage:    "new expiration date for the token (RFC3339 format, e.g. 2026-01-01T00:00:00Z); leave blank to keep existing",
				FlagName: "expires",
				Order:    80,
			},
		},
		scaffoldedit.SubroutineSet[string, types.Token]{
			SelectSub: func(id string) (item types.Token, err error) {
				return connection.Client.GetToken(id)
			},
			FetchSub: func() (items []types.Token, err error) {
				resp, err := connection.Client.ListTokens(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			},
			GetFieldSub: func(item types.Token, fieldKey string) (value string, err error) {
				switch fieldKey {
				case "expires":
					if item.ExpiresAt.IsZero() {
						return "", nil
					}
					return item.ExpiresAt.Format(time.RFC3339), nil
				}
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.Token, fieldKey, val string) (invalid string, err error) {
				if item == nil {
					return "", errors.New("cannot set nil item")
				}
				switch fieldKey {
				case "expires":
					if strings.TrimSpace(val) == "" {
						item.ExpiresAt = time.Time{}
						return "", nil
					}
					t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(val))
					if parseErr != nil {
						return "expires must be in RFC3339 format (e.g. 2026-01-01T00:00:00Z)", nil
					}
					item.ExpiresAt = t
				default:
					return "", fmt.Errorf("unknown field key: %v", fieldKey)
				}
				return
			},
			GetTitleSub: func(item types.Token) string {
				return item.Name
			},
			GetDescriptionSub: func(item types.Token) string {
				return item.Description
			},
			UpdateSub: func(data *types.Token) (identifier string, err error) {
				tr := types.TokenRegeneration{
					Expires: data.ExpiresAt,
				}
				tf, err := connection.Client.RegenToken(data.ID, tr)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s(full token:%s)", tf.ID, tf.Value), nil
			},
		},
	)
}
