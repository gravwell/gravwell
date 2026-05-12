package tree

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
)

// This file provides the tags action accessible at root.

func showTags() action.Pair {
	return scaffold.NewBasicAction("tags", "list all tags on the system",
		"Displays a list of all tags currently on the system."+
			" Tags are the basic categorization scheme Gravwell uses to organize data."+
			" See: https://docs.gravwell.io/ingesters/ingesters.html#tags",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			tags, err := connection.Client.GetTags()
			if err != nil {
				return err.Error(), nil
			}
			return strings.Join(tags, ", "), nil
		}, scaffold.BasicOptions{})
}
