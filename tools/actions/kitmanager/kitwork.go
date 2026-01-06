/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
)

// pullKit reaches out to the remote Gravwell instance and performs a kit build using the existing kit build request
// as a template.  It scans all the types and looks for any items that contain the kit label and adds them to the KBR.
// It then performs the build, initiates the download, and unpacks the kit to the local kit directory using kitctl.
func pullKit(cli *client.Client, kbrBase types.KitBuildRequest) (err error) {
	var kbr types.KitBuildRequest
	if kbr, err = generateKitBuildRequest(cli, kbrBase); err != nil {
		err = fmt.Errorf("failed to build kit build request: %w", err)
		return
	}

	fmt.Printf("Building kit %s version %v\n", kbr.ID, kbr.Version)
	var kresp types.KitBuildResponse
	if kresp, err = cli.BuildKit(kbr); err != nil {
		err = fmt.Errorf("failed to build kit: %w", err)
		return
	}
	// initiate the download request
	var resp *http.Response
	if resp, err = cli.KitDownloadRequest(kresp.UUID); err != nil {
		err = fmt.Errorf("failed to initiate kit download: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("kit download request failed: %s", resp.Status)
		return
	}
	// get a temp file up with our kit download
	var fout *os.File
	if fout, err = os.CreateTemp(os.TempDir(), kbr.ID); err != nil {
		err = fmt.Errorf("failed to create temp file for kit download: %w", err)
		return
	}
	pth := fout.Name() // get the file name for the temp file
	fmt.Printf("Downloading kit %s to %v\n", kbr.ID, pth)

	// stream the download to the file
	if _, err = io.Copy(fout, resp.Body); err != nil {
		err = fmt.Errorf("failed to download kit to temp file: %w", err)
		fout.Close()
		os.Remove(pth)
		return
	} else if err = fout.Close(); err != nil {
		os.Remove(pth)
		err = fmt.Errorf("failed to close kit temp file: %w", err)
		return
	}

	// call kitctl to unpack the kit to the target directory
	if err = unpackKitFile(pth, kitDir); err != nil {
		os.Remove(pth)
		err = fmt.Errorf("failed to unpack kit file: %w", err)
		return
	}

	//clean up the temporary file
	if lerr := os.Remove(pth); lerr != nil {
		fmt.Printf("Failed to remove temporary kit file %s: %v\n", pth, lerr)
	}

	fmt.Printf("Kit %s synced to %s\n", kbr.ID, kitDir)
	return
}

func generateKitBuildRequest(cli *client.Client, kbrBase types.KitBuildRequest) (kbr types.KitBuildRequest, err error) {
	// initialize the KBR with the things that are not going to change
	kbr = types.KitBuildRequest{
		ID:                kbrBase.ID,
		Name:              kbrBase.Name,
		Readme:            kbrBase.Readme,
		Version:           kbrBase.Version,
		MinVersion:        kbrBase.MinVersion,
		MaxVersion:        kbrBase.MaxVersion,
		EmbeddedItems:     kbrBase.EmbeddedItems,
		Icon:              kbrBase.Icon,
		Banner:            kbrBase.Banner,
		Cover:             kbrBase.Cover,
		Dependencies:      kbrBase.Dependencies,
		ConfigMacros:      kbrBase.ConfigMacros,
		ScriptDeployRules: kbrBase.ScriptDeployRules,
	}
	label := targetLabel(kbr.ID)
	// sweep through all the types in the kit build list and check for the appropriate labels on each
	// we also pass in the base build request so that we can also pickup any items in the original kit that
	// may not have the label.  This means that if a user wants to remove an item from the kit they must
	// remove the label and also remove it from kit build in kit archives.  This is a little cumbersome but
	// it's the only way to handle the initial kit build while also allowing people to add items to the kit
	// using the various GUI interfaces.

	//search library
	if err = getSearchLibraryItems(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//dashboards
	if err = getKitDashboards(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//templates
	if err = getKitTemplates(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//pivots
	if err = getKitPivots(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//resources
	if err = getKitResources(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//scheduled searches and scripts
	if err = getKitScheduledSearchesAndScripts(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//flows
	if err = getKitFlows(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//alerts
	if err = getKitAlerts(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//macros
	if err = getKitMacros(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//extractors
	if err = getKitExtractors(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//files
	if err = getKitFiles(cli, label, kbrBase, &kbr); err != nil {
		return
	}
	//playbooks
	if err = getKitPlaybooks(cli, label, kbrBase, &kbr); err != nil {
		return
	}

	// check the files and include banner, icon, and cover if not in the file set
	if kbr.Icon != "" {
		var icon uuid.UUID
		if icon, err = uuid.Parse(kbr.Icon); err != nil {
			err = fmt.Errorf("invalid Icon UUID %s: %w", kbr.Icon, err)
			return
		}
		if !containsUUID(kbr.Files, icon) {
			kbr.Files = append(kbr.Files, icon)
		}
	}
	if kbr.Banner != "" {
		var banner uuid.UUID
		if banner, err = uuid.Parse(kbr.Banner); err != nil {
			err = fmt.Errorf("invalid Banner UUID %s: %w", kbr.Banner, err)
			return
		}
		if !containsUUID(kbr.Files, banner) {
			kbr.Files = append(kbr.Files, banner)
		}
	}
	if kbr.Cover != "" {
		var cover uuid.UUID
		if cover, err = uuid.Parse(kbr.Cover); err != nil {
			err = fmt.Errorf("invalid Cover UUID %s: %w", kbr.Cover, err)
			return
		}
		if !containsUUID(kbr.Files, cover) {
			kbr.Files = append(kbr.Files, cover)
		}
	}

	// now that we have a constructed KitBuildRequest, bump the version number and perform the build
	kbr.Version++
	return
}

func unpackKitFile(pth, targetDir string) (err error) {
	// call the kitctl unpack command
	var stdoutStderr []byte
	cmd := exec.Command(kitCtl, "-zero-hash", "unpack", pth)
	// set working directory to target dir
	cmd.Dir = targetDir
	if stdoutStderr, err = cmd.CombinedOutput(); err != nil {
		err = fmt.Errorf("failed to unpack kit file %s: %v\nCommand Output: %s", pth, err, stdoutStderr)
	}
	return
}

func getSearchLibraryItems(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var items []types.WireSearchLibrary
	if items, err = cli.ListSearchLibrary(); err != nil {
		err = fmt.Errorf("failed to get search library items: %w", err)
		return
	}
	for _, sl := range items {
		if containsUUID(orig.SearchLibraries, sl.ThingUUID) || containsLabel(sl.Labels, label) {
			kbr.SearchLibraries = append(kbr.SearchLibraries, sl.ThingUUID)
		}
	}
	return
}

func getKitDashboards(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var dashboards []types.Dashboard
	if dashboards, err = cli.GetUserGroupsDashboards(); err != nil {
		err = fmt.Errorf("failed to get dashboards: %w", err)
		return
	}
	for _, d := range dashboards {
		if containsLabel(d.Labels, label) || containsUint64(orig.Dashboards, d.ID) {
			kbr.Dashboards = append(kbr.Dashboards, d.ID)
		}
	}
	return
}

func getKitTemplates(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var templates []types.WireUserTemplate
	if templates, err = cli.ListTemplates(); err != nil {
		err = fmt.Errorf("failed to get user templates: %w", err)
		return
	}
	for _, t := range templates {
		if containsLabel(t.Labels, label) || containsUUID(orig.Templates, t.ThingUUID) {
			kbr.Templates = append(kbr.Templates, t.ThingUUID)
		}
	}
	return
}

func getKitPivots(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var pivots []types.WirePivot
	if pivots, err = cli.ListPivots(); err != nil {
		err = fmt.Errorf("failed to get pivots: %w", err)
		return
	}
	for _, a := range pivots {
		if containsLabel(a.Labels, label) || containsUUID(orig.Pivots, a.GUID) {
			kbr.Pivots = append(kbr.Pivots, a.GUID)
		}
	}
	return
}

func getKitResources(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var resources []types.ResourceMetadata
	if resources, err = cli.GetResourceList(); err != nil {
		err = fmt.Errorf("failed to get resources: %w", err)
		return
	}
	for _, r := range resources {
		if containsLabel(r.Labels, label) || containsString(orig.Resources, r.GUID) {
			kbr.Resources = append(kbr.Resources, r.GUID)
		}
	}
	return
}

func getKitScheduledSearchesAndScripts(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var searches []types.ScheduledSearch
	if searches, err = cli.GetScheduledSearchList(); err != nil {
		err = fmt.Errorf("failed to get scheduled searches: %w", err)
		return
	}
	for _, ss := range searches {
		if containsLabel(ss.Labels, label) || containsInt32(orig.ScheduledSearches, ss.ID) {
			kbr.ScheduledSearches = append(kbr.ScheduledSearches, ss.ID)
		}
	}
	return
}

func getKitFlows(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var flows []types.ScheduledSearch
	if flows, err = cli.GetFlowList(); err != nil {
		err = fmt.Errorf("failed to get flows: %w", err)
		return
	}
	for _, f := range flows {
		if containsLabel(f.Labels, label) || containsInt32(orig.Flows, f.ID) {
			kbr.Flows = append(kbr.Flows, f.ID)
		}
	}
	return
}

func getKitAlerts(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var alerts []types.AlertDefinition
	if alerts, err = cli.GetAlerts(); err != nil {
		err = fmt.Errorf("failed to get alerts: %w", err)
		return
	}
	for _, a := range alerts {
		if containsLabel(a.Labels, label) || containsUUID(orig.Alerts, a.ThingUUID) {
			kbr.Alerts = append(kbr.Alerts, a.ThingUUID)
		}
	}
	return
}

func getKitMacros(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var macros []types.SearchMacro
	if macros, err = cli.GetUserGroupsMacros(); err != nil {
		err = fmt.Errorf("failed to get macros: %w", err)
		return
	}
	for _, m := range macros {
		if containsLabel(m.Labels, label) || containsUint64(orig.Macros, m.ID) {
			kbr.Macros = append(kbr.Macros, m.ID)
		}
	}
	return
}

func getKitExtractors(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var extractors []types.AXDefinition
	if extractors, err = cli.GetExtractions(); err != nil {
		err = fmt.Errorf("failed to get extractors: %w", err)
		return
	}
	for _, e := range extractors {
		if containsLabel(e.Labels, label) || containsUUID(orig.Extractors, e.UUID) {
			kbr.Extractors = append(kbr.Extractors, e.UUID)
		}
	}
	return
}

func getKitFiles(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var files []types.UserFileDetails
	if files, err = cli.UserFiles(); err != nil {
		err = fmt.Errorf("failed to get files: %w", err)
		return
	}
	for _, f := range files {
		if containsLabel(f.Labels, label) || containsUUID(orig.Files, f.ThingUUID) {
			kbr.Files = append(kbr.Files, f.ThingUUID)
		}
	}
	return
}

func getKitPlaybooks(cli *client.Client, label string, orig types.KitBuildRequest, kbr *types.KitBuildRequest) (err error) {
	var playbooks []types.Playbook
	if playbooks, err = cli.GetUserPlaybooks(); err != nil {
		err = fmt.Errorf("failed to get playbooks: %w", err)
		return
	}
	for _, p := range playbooks {
		if containsLabel(p.Labels, label) || containsUUID(orig.Playbooks, p.GUID) {
			kbr.Playbooks = append(kbr.Playbooks, p.GUID)
		}
	}
	return
}

// pushKit builds the kit from the kit directory and pushes it to the server
// pushKit DOES NOT increment the version number and does not depend on the remote system kit build process.
// It simply packs the local kit directory using kitctl and pushes it to the server for installation.
func pushKit(cli *client.Client, kbr types.KitBuildRequest) (err error) {
	fmt.Printf("Deploying kit %s version %v\n", kbr.ID, kbr.Version)
	// create a temp file for the kit
	var fout *os.File
	if fout, err = os.CreateTemp(os.TempDir(), kbr.ID); err != nil {
		err = fmt.Errorf("failed to create temp file for kit pack: %w", err)
		return
	}
	pth := fout.Name() // get the file name for the temp file
	if err = fout.Close(); err != nil {
		err = fmt.Errorf("failed to close temp kit pack file: %w", err)
		return
	}
	defer os.Remove(pth) // clean up the temp file when done

	// call the kitctl pack command
	var stdoutStderr []byte
	cmd := exec.Command(kitCtl, "pack", pth)
	cmd.Dir = kitDir // set working directory to kit dir
	if stdoutStderr, err = cmd.CombinedOutput(); err != nil {
		err = fmt.Errorf("failed to pack kit file %s: %v\nCommand Output: %s", pth, err, stdoutStderr)
		return
	}

	// push the kit to the server
	var state types.KitState
	if state, err = cli.UploadKit(pth); err != nil {
		err = fmt.Errorf("failed to upload kit file to server: %w", err)
		return
	}

	var kitLabels []string
	if kitLabels, err = getInstallLabels(); err != nil {
		err = fmt.Errorf("failed to get installation labels: %w", err)
		return
	}

	var groups, writeGroups []int32
	if groups, err = getInstallGroups(cli); err != nil {
		err = fmt.Errorf("failed to get installation groups: %w", err)
		return
	} else if writeGroups, err = getInstallWriteGroups(cli); err != nil {
		err = fmt.Errorf("failed to get installation write groups: %w", err)
		return
	}

	cfg := types.KitConfig{
		OverwriteExisting:  true,
		Global:             kitGlobal,
		ConfigMacros:       kbr.ConfigMacros,
		InstallationGroups: groups,
		Labels:             kitLabels,
		InstallationWriteAccess: types.Access{
			Global: kitWriteGlobal,
			GIDs:   writeGroups,
		},
	}

	// install the kit using the specified KitConfig values
	if err = cli.InstallKit(state.UUID, cfg); err != nil {
		err = fmt.Errorf("failed to install kit on server: %w", err)
		return
	}

	fmt.Printf("Kit %s deployed\n", kbr.ID)
	return
}
