package secrets

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// updateValue takes a single, new value to replace a secret's existing value.
func updateValue() action.Pair {
	cmd := treeutils.GenerateAction("update", "update a secret's value",
		"Update the value stored in a secret. The secret is identified by its ID.\n"+
			"Use "+ft.MutuallyExclusive([]string{"--value", "--file"})+" to provide the new value.", nil,
		func(c *cobra.Command, remaining []string) error {
			if c.Flags().NArg() != 1 {
				if !state.Interactive() {
					return errors.New(phrases.Exactly1ArgRequired("secret ID"))
				}
				return mother.Spawn(c.Root(), c, remaining)
			}

			value, noLength, inv, err := getUpdateValueFlags(c.Flags())
			if err != nil {
				return err
			} else if inv != "" {
				return errors.New(inv)
			} else if value == "" {
				if !state.Interactive() {
					return errors.New("you must supply --value or --file")
				}
				return mother.Spawn(c.Root(), c, remaining)
			}

			s, err := connection.Client.UpdateSecretValue(c.Flags().Arg(0), value)
			if err != nil {
				return err
			}
			success := "successfully updated secret " + s.Name
			if !noLength {
				success += fmt.Sprintf("(length: %dB)", len(value))
			}
			fmt.Fprintln(c.OutOrStdout(), success)
			return nil
		},
		treeutils.GenerateActionOptions{
			Usage:   ft.MutuallyExclusive([]string{"--value", "--file"}) + " " + ft.Mandatory("secret ID"),
			Example: "--value=mysupersecretvalue secret-one-two-three-four",
		},
	)

	cmd.Flags().AddFlagSet(updateValueFlags())

	return action.NewPair(cmd, NewUpdateModel())
}

func updateValueFlags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.String("value", "", "new value for the secret. Use --file if you are concerned about the value being in history")
	fs.String("file", "", "path to a file containing the new value for the secret. Takes the entire file contents as the secret value")
	fs.Bool("no-length", false, "do not print the length of the secret for confirmation")
	return fs
}

// Validates and fetches a value out of --value/--files.
// Returns an error if either flag fails or if both flags are given.
func getUpdateValueFlags(fs *pflag.FlagSet) (value string, noLength bool, inv string, _ error) {
	value, err := fs.GetString("value")
	if err != nil {
		return "", false, "", clilog.GetFlag(err)
	}
	file, err := fs.GetString("file")
	if err != nil {
		return "", false, "", clilog.GetFlag(err)
	}

	if noLength, err = fs.GetBool("no-length"); err != nil {
		clilog.GetFlag(err)
	}

	if value != "" && file != "" {
		return "", false, ft.ErrMutuallyExclusive("value", "file").Error(), nil
	}
	if value != "" {
		return value, noLength, "", nil
	}

	file = strings.TrimSpace(file)
	if file != "" {
		// dredge the secret value out of the file
		b, err := os.ReadFile(file)
		return string(b), noLength, "", err
	}
	return "", noLength, "", nil
}

type updateStage uint

const (
	selecting updateStage = iota
	inputting
	quitting
)

type updateModel struct {
	stage updateStage

	l          list.Model
	selectedID string

	valueTA    textarea.Model
	taSelected bool // if unset, submit is selected
	noLength   bool // do not display the length of the secret
}

func NewUpdateModel() *updateModel {
	m := &updateModel{}
	m.Reset()
	return m
}

func (m *updateModel) SetArgs(_ *pflag.FlagSet, args []string, width, height int) (inv string, onStart tea.Cmd, _ error) {
	fs := updateValueFlags()
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}

	if fs.NArg() > 1 {
		return phrases.Exactly1ArgRequired("secret ID"), nil, nil
	}

	if value, noLength, inv, err := getUpdateValueFlags(fs); err != nil {
		return "", nil, err
	} else if inv != "" {
		return inv, nil, nil
	} else {
		m.valueTA.SetValue(value)
		m.noLength = noLength
	}

	if fs.NArg() == 1 {
		m.selectedID = fs.Arg(0)
		if value := m.valueTA.Value(); value != "" { // submit directly
			m.stage = quitting
			s, err := connection.Client.UpdateSecretValue(m.selectedID, value)
			if err != nil {
				return "", nil, err
			}
			success := "successfully updated secret " + s.Name
			if !m.noLength {
				success += fmt.Sprintf("(length: %dB)", len(value))
			}
			return "", tea.Println(success), nil
		}
		// skip list
		m.stage = inputting
	}

	if m.stage == selecting {
		lr, err := connection.Client.ListSecrets(&types.QueryOptions{AdminMode: connection.AdminMode()})
		if err != nil {
			return "", nil, err
		}
		if len(lr.Results) < 1 {
			return "", nil, errors.New("you have no available secrets")
		}
		items := make([]list.Item, len(lr.Results))
		for i, secret := range lr.Results {
			items[i] = &listitem.Generic{
				ID_:        secret.ID,
				Name:       secret.Name,
				SecondLine: secret.Description,
			}
		}

		m.l = stylesheet.NewList(items, width, height, "secret", "secrets")
	}
	return "", nil, nil
}

func (m *updateModel) Update(msg tea.Msg) tea.Cmd {
	switch m.stage {
	case quitting:
		return nil
	case selecting:
		if hotkeys.ButtonPressed(msg) {
			s, err := listitem.GetGeneric(&m.l)
			if err != nil {
				m.stage = quitting
				return tea.Println(err.Error())
			}
			m.selectedID = s.ID()
			m.stage = inputting
			return nil
		}
		var cmd tea.Cmd
		m.l, cmd = m.l.Update(msg)
		return cmd
	case inputting:
		idx := 0
		if !m.taSelected {
			idx = 1
		}
		handled, _, newIndex := hotkeys.MoveCursor(msg, uint(idx), 2, &m.valueTA)
		if handled {
			switch newIndex {
			case 0:
				m.taSelected = true
				m.valueTA.Focus()
			case 1:
				m.taSelected = false
				m.valueTA.Blur()
			default:
				m.stage = quitting
				clilog.Writer.Error("MoveCursor returned an unusable index",
					log.KV("index", newIndex),
					log.KV("msg", msg),
					log.KV("prior index", idx))
				return tea.Println(clilog.ErrInternal{}.Error())
			}
			return nil
		} else if hotkeys.ButtonPressed(msg) && !m.taSelected { // submit
			m.stage = quitting
			value := m.valueTA.Value()
			s, err := connection.Client.UpdateSecretValue(m.selectedID, value)
			if err != nil {
				return tea.Println(err.Error())
			}
			success := "successfully updated secret " + s.Name
			if !m.noLength {
				success += fmt.Sprintf("(length: %dB)", len(value))
			}
			return tea.Println(success)
		}
		var cmd tea.Cmd
		m.valueTA, cmd = m.valueTA.Update(msg)
		return cmd
	default:
		clilog.Writer.Error("unknown stage", log.KV("stage", m.stage))
		m.stage = quitting
		return tea.Println(clilog.ErrInternal{}.Error())
	}
}

func (m *updateModel) View() string {
	switch m.stage {
	case quitting:
		return ""
	case selecting:
		return m.l.View()
	case inputting:
		return fmt.Sprintf("%s\n%s\n\n%s",
			stylesheet.Cur.FieldText.Render("New secret value:"),
			m.valueTA.View(),
			stylesheet.ViewSubmitButton(!m.taSelected, m.valueTA.Width()),
		)
	default:
		clilog.Writer.Error("unknown stage", log.KV("stage", m.stage))
		m.stage = quitting
		return clilog.ErrInternal{}.Error()
	}
}

func (m *updateModel) Done() bool {
	return m.stage == quitting
}

func (m *updateModel) Reset() error {
	m.stage = selecting

	m.l = list.Model{}
	m.selectedID = ""

	m.valueTA = textarea.New()
	m.taSelected = true
	m.valueTA.Focus()

	m.noLength = false

	return nil
}
