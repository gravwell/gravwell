/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package create

import (
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/connection"
	"gwcli/stylesheet"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/scaffold/scaffoldcreate"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/spf13/pflag"
)

const (
	kname   = "name"
	kdesc   = "desc"
	kmodule = "module"
	ktags   = "tags"
	kparams = "params"
	kargs   = "args"
	klabels = "labels"
)

func NewExtractorsCreateAction() action.Pair {
	fields := scaffoldcreate.Config{
		kname: scaffoldcreate.Field{
			Required:      true,
			Title:         "name",
			Usage:         "name of the new extractor",
			Type:          scaffoldcreate.Text,
			FlagName:      "name",
			FlagShorthand: 'n',
			Order:         100,
		},
		kdesc: scaffoldcreate.Field{
			Required:      true,
			Title:         "description",
			Usage:         "description of the new extractor",
			Type:          scaffoldcreate.Text,
			FlagName:      "desc",
			FlagShorthand: 'd',
			Order:         90,
		},
		kmodule: scaffoldcreate.Field{
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
			/*CustomTIFuncSetArg: func(ti *textinput.Model) textinput.Model {
				// TODO move this.... somewhere as it depends on the tag?

				// fetch current labels as suggestions
				if mp, err := connection.Client.ExploreGenerate(); err != nil {
					clilog.Writer.Warnf("failed to fetch ax label map: %v", err)
					ti.ShowSuggestions = false
				} else {
					suggest := make([]string, len(mp))
					i := 0
					for k, _ := range mp {
						suggest[i] = k
						i += 1
					}
					ti.SetSuggestions(suggest)
					ti.ShowSuggestions = true
				}

				return ti
			}, */

		},
		ktags: scaffoldcreate.Field{
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
		kparams: scaffoldcreate.Field{
			Required:     false,
			Title:        "params/regex",
			Usage:        "",
			Type:         scaffoldcreate.Text,
			FlagName:     "params",
			DefaultValue: "",

			Order: 60,
		},
		kargs: scaffoldcreate.Field{
			Required:     false,
			Title:        "arguments/options",
			Usage:        "arguments/options on this ax",
			Type:         scaffoldcreate.Text,
			FlagName:     "args",
			DefaultValue: "",

			Order: 50,
		},
		klabels: scaffoldcreate.Field{
			Required:     false,
			Title:        "labels/categories",
			Usage:        "arguments/options on this ax",
			Type:         scaffoldcreate.Text,
			FlagName:     "labels",
			DefaultValue: "",
		},
	}

	return scaffoldcreate.NewCreateAction("extractor", fields, create, func() (fs pflag.FlagSet) {
		fs.Bool(ft.Name.Dryrun, false, ft.Usage.Dryrun)
		return fs
	})
}

func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values, fs *pflag.FlagSet) (any, string, error) {
	// no need to nil check; Required boolean enforces that for us
	axd := types.AXDefinition{
		Name:   vals[kname],
		Desc:   vals[kdesc],
		Module: vals[kmodule],
		Tags:   strings.Split(strings.Replace(vals[ktags], " ", "", -1), ","),
		Params: vals[kparams],
		Args:   vals[kargs],
		Labels: strings.Split(strings.Replace(vals[klabels], " ", "", -1), ","),
	}

	// check for dryrun
	var (
		dr  bool
		err error
	)
	if dr, err = fs.GetBool(ft.Name.Dryrun); err != nil {
		return 0, "", err
	}

	var (
		id  uuid.UUID
		wrs []types.WarnResp
	)
	if dr {
		wrs, err = connection.Client.TestAddExtraction(axd)
		id = uuid.Nil
	} else {
		id, wrs, err = connection.Client.AddExtraction(axd)
	}

	if len(wrs) > 0 {
		var invSB strings.Builder
		for _, wr := range wrs {
			invSB.WriteString(fmt.Sprintf("%v: %v\n", wr.Name, wr.Err))
		}
		return 0, invSB.String(), nil
	}

	return id, "", err
}
