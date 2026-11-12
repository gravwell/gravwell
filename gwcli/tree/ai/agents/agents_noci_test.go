//go:build noci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package agents_test

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
)

const (
	username, password string = "admin", "changeme"
)

var ( // set by init
	server string
	meta   []string
)

func init() {
	server = testsupport.Server()
	meta = []string{"--insecure", "-x", "-u", username, "--server=" + server}
}

// Upload an agent, find it in list, show it, edit its metadata, replace its graph, download it, and delete it.
func TestUploadEditShowReplaceDownloadDelete(t *testing.T) {
	tDir := t.TempDir()
	t.Setenv("GRAVWELL_PASSWORD", password)

	var (
		agentName   = "gwcli-" + randomdata.SillyName()
		agentDesc   = "from " + t.Name()
		agentLabels = []string{"lbl1", "otherlbl"}
	)
	def := types.Agent{
		CommonFields: types.CommonFields{Name: agentName, Description: agentDesc},
		Entrypoint: types.NodeSpec{Type: types.NodeSerial, Name: "pipeline", Inherit: true, Children: []*types.NodeSpec{
			{Type: types.NodeSingle, Name: "collect", Prompt: "collect the facts", AllowedTools: []string{"list_tags"}, MaxIterations: 5},
			{Type: types.NodeSingle, Name: "report", Prompt: "write it up", Inherit: true},
		}},
	}
	filePath := writeDefinition(t, path.Join(tDir, "agent.json"), def)

	execute(t, "ai", "agents", "upload", "-f", filePath, "--labels", strings.Join(agentLabels, ","))

	// check that list pulls back the new agent with its graph summarized
	row := listForItem(t, agentName)
	agentID := row.ID
	if row.Description != agentDesc {
		t.Error("retrieved incorrect description", testsupport.ExpectedActual(agentDesc, row.Description))
	}
	if !testsupport.SlicesUnorderedEqual(row.Labels, agentLabels) {
		t.Error("assigned labels do not match given labels", testsupport.ExpectedActual(agentLabels, row.Labels))
	}
	if row.Type != types.NodeSerial || row.Nodes != 3 {
		t.Error("incorrect type or node count", testsupport.ExpectedActual("serial 3", row))
	}

	// check that show describes the graph
	{
		out := execute(t, "ai", "agents", "show", agentID)
		for _, want := range []string{
			agentName + " (" + agentID + ")",
			"- serial pipeline [inherits tools]",
			"  - single collect [tools: list_tags; max iterations: 5;",
			"  - single report [inherits tools;",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("show output is missing %q:\n%s", want, out)
			}
		}
		var ep types.NodeSpec
		if err := json.Unmarshal([]byte(execute(t, "ai", "agents", "show", "--json", agentID)), &ep); err != nil {
			t.Fatal("show --json did not print a node graph: ", err)
		} else if ep.Name != "pipeline" || len(ep.Children) != 2 {
			t.Errorf("show --json printed %+v", ep)
		}
	}

	// check that we can alter its metadata without touching the graph
	newDesc, newLabels := "altered", []string{"edited"}
	execute(t, "ai", "agents", "edit", "-i", agentID, "--description="+newDesc, "--labels="+strings.Join(newLabels, ","))
	row = listForItem(t, agentName)
	if row.ID != agentID {
		t.Error("incorrect agent ID", testsupport.ExpectedActual(agentID, row.ID))
	}
	if row.Description != newDesc {
		t.Error("incorrect description", testsupport.ExpectedActual(newDesc, row.Description))
	}
	if !testsupport.SlicesUnorderedEqual(row.Labels, newLabels) {
		t.Error("incorrect labels set by edit", testsupport.ExpectedActual(newLabels, row.Labels))
	}
	if row.Nodes != 3 {
		t.Error("edit changed the graph", testsupport.ExpectedActual(3, row.Nodes))
	}

	// check that replace swaps the graph, and that download returns it
	{
		def.Entrypoint = types.NodeSpec{Type: types.NodeSingle, Name: "solo", Prompt: "do it all", Inherit: true}
		execute(t, "ai", "agents", "replace", "-f", writeDefinition(t, path.Join(tDir, "replace.json"), def), agentID)

		resultPath := path.Join(tDir, "download.json")
		execute(t, "ai", "agents", "download", "-o", resultPath, agentID)
		b, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal("failed to read download: ", err)
		}
		var dl types.Agent
		if err := json.Unmarshal(b, &dl); err != nil {
			t.Fatal("download is not an agent definition: ", err)
		}
		if dl.Name != agentName || dl.Entrypoint.Type != types.NodeSingle || dl.Entrypoint.Name != "solo" || dl.Entrypoint.Prompt != "do it all" {
			t.Errorf("downloaded %+v", dl)
		}
	}

	execute(t, "ai", "agents", "delete", agentID)
	if findRow(listRows(t), agentName) != nil {
		t.Errorf("agent %v still listed after delete", agentID)
	}
}

func writeDefinition(t *testing.T, p string, a types.Agent) string {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	// the file format is the definition only; drop the server-managed fields Marshal emits
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	def := map[string]any{"Name": m["Name"], "Description": m["Description"], "Entrypoint": m["Entrypoint"]}
	if b, err = json.Marshal(def); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

// execute runs gwcli with the given arguments, failing the test on a non-zero exit, and returns stdout.
func execute(t *testing.T, args ...string) string {
	t.Helper()
	var sbOut, sbErr strings.Builder
	if ec := tree.Execute(append(meta, args...), tree.ExecuteOptions{Stdout: &sbOut, Stderr: &sbErr}); ec != 0 {
		t.Fatalf("%v: bad error code %d. STDERR: %s", args, ec, sbErr.String())
	}
	return sbOut.String()
}

const listColumns = "CommonFields.ID,CommonFields.Name,CommonFields.Description,CommonFields.Labels,Type,Nodes"

// listedAgent is a row of "list --json" with listColumns.
type listedAgent struct {
	ID, Name, Description string
	Labels                []string
	Type                  types.NodeType
	Nodes                 int
}

// listRows executes "list" and returns its rows.
// It reads JSON rather than CSV, as the CSV output does not quote descriptions that contain commas.
func listRows(t *testing.T) []listedAgent {
	t.Helper()
	resultPath := path.Join(t.TempDir(), "list.json")
	execute(t, "ai", "agents", "list", "--json", "-o", resultPath, "--columns", listColumns)
	b, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []listedAgent
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func findRow(rows []listedAgent, name string) *listedAgent {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}

// listForItem executes "list" and returns the row with the given name.
func listForItem(t *testing.T, name string) listedAgent {
	t.Helper()
	rows := listRows(t)
	row := findRow(rows, name)
	if row == nil {
		t.Fatalf("found no rows with name %v. Rows: %v", name, rows)
	}
	return *row
}
