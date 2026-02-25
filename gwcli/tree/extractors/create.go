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
	createNameKey   = "name"
	createDescKey   = "desc"
	createModuleKey = "module"
	createTagsKey   = "tags"
	createParamsKey = "params"
	createArgsKey   = "args"
	createLabelsKey = "labels"
)

func newExtractorsCreateAction() action.Pair {
	fields := scaffoldcreate.Config{
		createNameKey: scaffoldcreate.Field{
			Required:      true,
			Title:         "name",
			Usage:         "name of the new extractor",
			Type:          scaffoldcreate.Text,
			FlagName:      "name",
			FlagShorthand: 'n',
			Order:         100,
		},
		createDescKey: scaffoldcreate.Field{
			Required:      true,
			Title:         "description",
			Usage:         ft.Description.Usage("extractor"),
			Type:          scaffoldcreate.Text,
			FlagName:      ft.Description.Name(),
			FlagShorthand: 'd',
			Order:         90,
		},
		createModuleKey: scaffoldcreate.Field{
			Required: true,
			Title:    "module",
			Usage: "extraction module to use. Available modules:\n" +
				"ax, canbus, cef, csv, dump, fields, grok, intrinsic, ip, ipfix, j1939, json, " +
				"kv, netflow, packet, packetlayer, path, regex, slice, strings, subnet, syslog, " +
				"winlog, xml",
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
		createTagsKey: scaffoldcreate.Field{
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
		createParamsKey: scaffoldcreate.Field{
			Required:     false,
			Title:        "params/regex",
			Usage:        "",
			Type:         scaffoldcreate.Text,
			FlagName:     "params",
			DefaultValue: "",

			Order: 60,
		},
		createArgsKey: scaffoldcreate.Field{
			Required:     false,
			Title:        "arguments/options",
			Usage:        "arguments/options on this ax",
			Type:         scaffoldcreate.Text,
			FlagName:     "args",
			DefaultValue: "",

			Order: 50,
		},
		createLabelsKey: scaffoldcreate.Field{
			Required:     false,
			Title:        "labels/categories",
			Usage:        "arguments/options on this ax",
			Type:         scaffoldcreate.Text,
			FlagName:     "labels",
			DefaultValue: "",
		},
	}

	return scaffoldcreate.NewCreateAction("extractor", fields, create, func() (fs pflag.FlagSet) {
		ft.Dryrun.Register(&fs)
		return fs
	})
}

func create(_ scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (any, string, error) {
	// no need to nil check; Required boolean enforces that for us

	// map fields back into the underlying type
	axd := types.AX{
		CommonFields: types.CommonFields{
			Name:        fieldValues[createNameKey],
			Description: fieldValues[createDescKey],
			Labels:      strings.Split(strings.ReplaceAll(fieldValues[createLabelsKey], " ", ""), ","),
		},
		Module: fieldValues[createModuleKey],
		Tags:   strings.Split(strings.ReplaceAll(fieldValues[createTagsKey], " ", ""), ","),
		Params: fieldValues[createParamsKey],
		Args:   fieldValues[createArgsKey],
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
