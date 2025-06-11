package ingest

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/spf13/pflag"
)

var Ingest action.Model = Initial()

type ingest struct{}

func Initial() *ingest {
	i := &ingest{}

	// TODO

	return i
}

func (i *ingest) Update(msg tea.Msg) tea.Cmd { return nil }

func (i *ingest) View() string { return "" }

func (i *ingest) Done() bool { return true }

func (i *ingest) Reset() error { return nil }

func (i *ingest) SetArgs(_ *pflag.FlagSet, tokens []string) (string, tea.Cmd, error) {
	return "", nil, nil
}
