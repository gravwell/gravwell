/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package license provides actions for viewing and updating the Gravwell license.
package license

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	borderWidth int = 48
	fieldWidth  int = 16
)

func NewNav() *cobra.Command {
	const (
		use   string = "license"
		short string = "view and manage the Gravwell license"
		long  string = "License provides actions for inspecting the current license and uploading a new one."
	)
	return treeutils.GenerateNav(use, short, long, nil,
		nil,
		[]action.Pair{
			licenseInfo(),
			licenseFeatures(),
			licenseSKU(),
			licenseSerial(),
			licenseUpdate(),
		},
	)
}

func licenseInfo() action.Pair {
	const (
		short string = "display information about the current license"
		long  string = "Displays details about the currently installed Gravwell license."
	)
	return scaffoldlist.NewListAction(short, long,
		types.LicenseInfo{},
		func(fs *pflag.FlagSet) ([]types.LicenseInfo, error) {
			li, err := connection.Client.GetLicenseInfo()
			if err != nil {
				return nil, err
			}
			return []types.LicenseInfo{li}, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{Use: "info"},
			DefaultColumns: []string{
				"Type",
				"Version",
				"Expiration",
			},
			Pretty: func(_ []string, _ map[string]string) (string, error) {
				li, err := connection.Client.GetLicenseInfo()
				if err != nil {
					return "", err
				}
				body := fmt.Sprintf("%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v\n"+
					"%s%v",
					stylesheet.Cur.Field("Type", fieldWidth), li.Type,
					stylesheet.Cur.Field("Serial", fieldWidth), li.Serial(),
					stylesheet.Cur.Field("SKU", fieldWidth), li.SKU(),
					stylesheet.Cur.Field("Customer UUID", fieldWidth), li.CustomerUUID,
					stylesheet.Cur.Field("Customer #", fieldWidth), li.CustomerNumber,
					stylesheet.Cur.Field("Expiration", fieldWidth), li.Expiration,
					stylesheet.Cur.Field("Max Nodes", fieldWidth), li.MaxNodes,
					stylesheet.Cur.Field("NFR", fieldWidth), li.NFR,
					stylesheet.Cur.Field("Version", fieldWidth), li.Version)
				return stylesheet.SegmentedBorder(
					stylesheet.Cur.ComposableSty.ComplimentaryBorder.BorderForeground(stylesheet.Cur.PrimaryText.GetForeground()),
					borderWidth,
					struct {
						StylizedTitle string
						Contents      string
					}{
						stylesheet.Cur.TertiaryText.Bold(true).Render(" License "),
						body,
					},
				)
			},
		},
	)
}

func licenseFeatures() action.Pair {
	const (
		use   string = "features"
		short string = "display enabled features of the current license"
		long  string = "Displays the comma-separated list of features enabled on the currently installed Gravwell license."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			li, err := connection.Client.GetLicenseInfo()
			if err != nil {
				return err.Error(), nil
			}
			feats := li.Features()
			checks := []struct {
				enabled bool
				name    string
			}{
				{feats.Replication, types.ReplicationName},
				{feats.SingleSignon, types.SingleSignonName},
				{feats.Overwatch, types.OverwatchName},
				{feats.NoStats, types.NoStatsName},
				{feats.UnlimitedCPU, types.UnlimitedCPUName},
				{feats.CBAC, types.CBACName},
				{feats.UnlimitedIngest, types.UnlimitedIngestName},
				{feats.LogbotLLM, types.LogbotLLMName},
			}
			var enabled []string
			for _, c := range checks {
				if c.enabled {
					enabled = append(enabled, c.name)
				}
			}
			if len(enabled) == 0 {
				return "(none)", nil
			}
			return strings.Join(enabled, ", "), nil
		},
		scaffold.BasicOptions{},
	)
}

func licenseSKU() action.Pair {
	const (
		use   string = "sku"
		short string = "display the SKU of the current license"
		long  string = "Displays the SKU string for the currently installed Gravwell license."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			sku, err := connection.Client.GetLicenseSKU()
			if err != nil {
				return err.Error(), nil
			}
			return sku, nil
		},
		scaffold.BasicOptions{},
	)
}

func licenseSerial() action.Pair {
	const (
		use   string = "serial"
		short string = "display the serial number of the current license"
		long  string = "Displays the serial number for the currently installed Gravwell license."
	)
	return scaffold.NewBasicAction(use, short, long,
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			serial, err := connection.Client.GetLicenseSerial()
			if err != nil {
				return err.Error(), nil
			}
			return serial, nil
		},
		scaffold.BasicOptions{},
	)
}

func licenseUpdate() action.Pair {
	return scaffoldcreate.NewCreateAction("license",
		map[string]scaffoldcreate.Field{
			"path": scaffoldcreate.FieldPath("license file"),
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			path := fields["path"].Provider.Get()

			warnings, err := connection.Client.UploadLicenseFile(path)
			if err != nil {
				return nil, err.Error(), nil
			}

			if len(warnings) == 0 {
				return "license updated successfully", "", nil
			}

			// surface any per-indexer warnings
			msgs := make([]string, 0, len(warnings))
			for _, w := range warnings {
				msgs = append(msgs, fmt.Sprintf("%s: %s", w.Name, w.Err))
			}
			return "license updated with warnings:\n" + strings.Join(msgs, "\n"), "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "update",
			},
		},
	)
}
