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
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
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
		nil,
		scaffoldlist.Options{
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
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
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "get",
				Example: "get ID1 ID2",
			},
			Pretty: func(_ []string, _ map[string]string) (string, error) {
				// find the longest ID to use as the width
				var longestIDLen int
				for _, tkn := range tokens {
					if l := len(tkn.ID); l > longestIDLen {
						longestIDLen = l
					}
				}

				var sb strings.Builder
				for _, tkn := range tokens {
					sb.WriteString(prettyToken(tkn, longestIDLen))
				}
				return sb.String(), nil
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

// prettyTokens pretty-prints the given token and returns it.
// Tokens are printed using the segmented border helper.
func prettyToken(t types.Token, longestIDLen int) string {
	const (
		fieldWidth = 12
	)

	var identitySb strings.Builder
	identitySb.WriteString(stylesheet.Cur.Field("ID", fieldWidth) + t.ID + "\n")
	identitySb.WriteString(stylesheet.Cur.Field("Name", fieldWidth) + t.Name + "\n")
	if t.Description != "" {
		identitySb.WriteString(stylesheet.Cur.Field("Description", fieldWidth) + t.Description + "\n")
	}
	identitySb.WriteString(stylesheet.Cur.Field("Owner", fieldWidth) + fmt.Sprintf("%v", t.Owner.Name) + "\n")
	identitySb.WriteString(stylesheet.Cur.Field("Created", fieldWidth) + t.CreatedAt.Format("2006-01-02 15:04:05 UTC") + "\n")
	identitySb.WriteString(stylesheet.Cur.Field("Updated", fieldWidth) + t.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))

	var expirySb strings.Builder
	expirySb.WriteString(stylesheet.Cur.Field("Expires", fieldWidth) + t.ExpiresString())
	if t.Expired() {
		expirySb.WriteString(" " + stylesheet.Cur.ErrorText.Render("(EXPIRED)"))
	} else if !t.ExpiresAt.IsZero() {
		expirySb.WriteString(" " + stylesheet.Cur.SecondaryText.Render("(active)"))
	}

	var capsSb strings.Builder
	if len(t.Capabilities) == 0 {
		capsSb.WriteString(stylesheet.Cur.TertiaryText.Render("(none)"))
	} else {
		for i, cap := range t.Capabilities {
			capsSb.WriteString(stylesheet.Cur.PrimaryText.Render(cap))
			if i < len(t.Capabilities)-1 {
				capsSb.WriteString("\n")
			}
		}
	}

	type section = struct {
		StylizedTitle string
		Contents      string
	}

	sectionHeader := func(str string) string {
		return stylesheet.Cur.TertiaryText.Bold(true).Render(str)
	}
	subSectionHeader := func(str string) string {
		return stylesheet.Cur.SecondaryText.Bold(true).Render(str)
	}

	sections := []section{
		{StylizedTitle: sectionHeader(" " + t.Name + " ")},
		{StylizedTitle: " " + subSectionHeader("Identity") + " ", Contents: identitySb.String()},
		{StylizedTitle: " " + subSectionHeader("Expiry") + " ", Contents: expirySb.String()},
		{
			StylizedTitle: " " + subSectionHeader(fmt.Sprintf("Capabilities[%d]", len(t.Capabilities))) + " ",
			Contents:      capsSb.String(),
		},
	}

	if len(t.Labels) > 0 {
		sections = append(sections, section{
			StylizedTitle: " " + subSectionHeader("Labels") + " ",
			Contents:      stylesheet.Cur.TertiaryText.Render(strings.Join(t.Labels, ", ")),
		})
	}

	s, err := stylesheet.SegmentedBorder(
		stylesheet.Cur.ComposableSty.ComplimentaryBorder.BorderForeground(stylesheet.Cur.PrimaryText.GetForeground()),
		fieldWidth+longestIDLen+3,
		sections...,
	)
	if err != nil {
		clilog.Writer.Warnf("failed to generate token view: %v", err)
		return "failed to display token"
	}
	return s
}

const defaultTokenPath = "token"

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name": scaffoldcreate.FieldName("token"),
		"desc": scaffoldcreate.FieldDescription("token"),
		"capabilities": {
			Required: false,
			Title:    "Capabilities",
			Flag:     scaffoldcreate.FlagConfig{Usage: "comma-separated list of capabilities to grant the token", Shorthand: 'c'},
			Provider: scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
				ListOptions: multiselectlist.Options{HideDescription: true},
				SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
					caps, err := connection.Client.TokenCapabilities()
					if err != nil {
						clilog.Writer.Error("failed to fetch token capabilities:", log.KVErr(err))
						return nil
					}
					itms := make([]multiselectlist.SelectableItem[string], len(caps))
					for i, cap := range caps {
						itms[i] = &multiselectlist.Item{
							Title_: cap,
							ID_:    cap,
						}
					}
					return itms
				},
			}),
			Order: 80,
		},
		"expires": {
			Required: false,
			Title:    "Expires At",
			Flag:     scaffoldcreate.FlagConfig{Usage: "expiration date for the token (RFC3339 format, e.g. 2026-01-01T00:00:00Z); leave blank for no expiry", Shorthand: 'e'},
			Provider: &scaffoldcreate.TextProvider{},
			Order:    60,
		},
	}

	return scaffoldcreate.NewCreateAction("token", fields,
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			tc := types.TokenCreate{
				Name:        cfg["name"].Provider.Get(),
				Description: cfg["desc"].Provider.Get(),
			}

			if caps := cfg["capabilities"].Provider.Get(); strings.TrimSpace(caps) != "" {
				raw := strings.Split(strings.TrimSpace(caps), ",")
				tc.Capabilities = make([]string, 0, len(raw))
				for _, c := range raw {
					if trimmed := strings.TrimSpace(c); trimmed != "" {
						tc.Capabilities = append(tc.Capabilities, trimmed)
					}
				}
			}

			if exp := cfg["expires"].Provider.Get(); strings.TrimSpace(exp) != "" {
				t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(exp))
				if parseErr != nil {
					return "", "expires must be in RFC3339 format (e.g. 2026-01-01T00:00:00Z)", nil
				}
				tc.ExpiresAt = t
			}

			// open up the output file
			out, err := fs.GetString("out")
			if err != nil {
				return "", "", err
			}
			// check if a file already exists; we definitely don't want to clobber it.
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				return "", "", err
			}

			outFile, err := os.Create(out)
			if err != nil {
				return "", "", err
			}
			tf, err := connection.Client.CreateToken(tc)
			if err != nil {
				return "", "", err
			}
			_, err = outFile.WriteString(tf.Value)
			return tf.ID, "", err
		}, scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := pflag.NewFlagSet("TokenOut", pflag.ContinueOnError)
					// ! does not use the standard path or out ft flags because this one has special requirements
					fs.StringP("out", "o", defaultTokenPath, "file to write the token value to. "+
						"To prevent the accidental loss of a token, token creation will be aborted if a file is found at this path. "+
						lipgloss.NewStyle().Italic(true).Render("-o will not clobber existing files."))
					return fs
					// long: "Create a new token." +
					// "The token itself will be written to local file '" + stylesheet.Cur.ExampleText.Render(defaultTokenPath) + "' unless -o is specified."
				},
			},
		})
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
