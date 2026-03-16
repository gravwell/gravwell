/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package extractors

import (
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

const (
	fieldKeyName   = "name"
	fieldKeyDesc   = "desc"
	fieldKeyModule = "module"
	fieldKeyTags   = "tags"
	fieldKeyParams = "params"
	fieldKeyArgs   = "args"
	fieldKeyLabels = "labels"
)

func newExtractorsCreateAction() action.Pair {
	fields := scaffoldcreate.Config{
		fieldKeyName: scaffoldcreate.FieldName("extractor"),
		fieldKeyDesc: scaffoldcreate.FieldDescription("extractor"),
		fieldKeyModule: scaffoldcreate.Field{
			Required:      true,
			Title:         "module",
			Usage:         "extraction module to use. Call `engines` to list available options.",
			Type:          scaffoldcreate.Text,
			FlagName:      "module",
			FlagShorthand: 'm',
			DefaultValue:  "",
			Order:         80,
			CustomTIFuncInit: func() textinput.Model {
				// manually add suggestions based on
				// docs.gravwell.io/search/extractionmodules.html#search-module-documentation
				ti := stylesheet.NewTI("", false)
				ti.ShowSuggestions = true
				ti.SetSuggestions([]string{"ax", "canbus", "cef", "csv", "dump", "fields", "grok",
					"intrinsic", "ip", "ipfix", "j1939", "json", "kv", "netflow", "packet",
					"packetlayer", "path", "regex", "slice", "strings", "subnet", "syslog",
					"winlog", "xml"})
				return ti
			},
		},
		fieldKeyTags: scaffoldcreate.Field{
			Required:      true,
			Title:         "tags",
			Usage:         "tags this ax will extract from. There can only be one extractor per tag.",
			Type:          scaffoldcreate.Text,
			FlagName:      "tags",
			FlagShorthand: 't',
			DefaultValue:  "",
			Order:         70,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "tag1,tag2,tag3"
				return ti
			},
			CustomTIFuncSetArg: func(ti *textinput.Model) textinput.Model {
				if tags, err := connection.Client.GetTags(); err != nil {
					clilog.Writer.Warnf("failed to fetch tags: %v", err)
					ti.ShowSuggestions = false
				} else {
					ti.ShowSuggestions = true
					ti.SetSuggestions(tags)
				}

				return *ti
			},
		},
		fieldKeyParams: scaffoldcreate.Field{
			Required:     false,
			Title:        "params/regex",
			Usage:        "",
			Type:         scaffoldcreate.Text,
			FlagName:     "params",
			DefaultValue: "",

			Order: 60,
		},
		fieldKeyArgs: scaffoldcreate.Field{
			Required:     false,
			Title:        "arguments/options",
			Usage:        "arguments/options on this ax",
			Type:         scaffoldcreate.Text,
			FlagName:     "args",
			DefaultValue: "",

			Order: 50,
		},
		fieldKeyLabels: scaffoldcreate.FieldLabels(),
	}

	return scaffoldcreate.NewCreateAction("extractor", fields, create,
		scaffoldcreate.Options{
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				ft.Dryrun.Register(&fs)
				return fs
			},
		})
}

func create(_ scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (any, string, error) {
	// no need to nil check; Required boolean enforces that for us

	// map fields back into the underlying type
	axd := types.AX{
		CommonFields: types.CommonFields{
			Name:        fieldValues[fieldKeyName],
			Description: fieldValues[fieldKeyDesc],
			Labels:      strings.Split(strings.ReplaceAll(fieldValues[fieldKeyLabels], " ", ""), ","),
		},
		Module: fieldValues[fieldKeyModule],
		Tags:   strings.Split(strings.ReplaceAll(fieldValues[fieldKeyTags], " ", ""), ","),
		Params: fieldValues[fieldKeyParams],
		Args:   fieldValues[fieldKeyArgs],
	}

	// check for dryrun
	var (
		dr  bool
		err error
	)
	if dr, err = fs.GetBool(ft.Dryrun.Name()); err != nil {
		return 0, "", err
	}

	var (
		id  string
		wrs []types.WarnResp
	)
	if dr {
		wrs, err = connection.Client.TestAddExtraction(axd)
	} else {
		axd, wrs, err = connection.Client.AddExtraction(axd)
		id = axd.ID
	}

	if len(wrs) > 0 {
		var invSB strings.Builder
		for _, wr := range wrs {
			fmt.Fprintf(&invSB, "%v: %v\n", wr.Name, wr.Err)
		}
		return 0, invSB.String(), nil
	}

	return id, "", err
}
