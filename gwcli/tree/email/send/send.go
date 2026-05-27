// Package send introduces an action for sending emails.
package send

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/validate"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewPair() action.Pair {
	cmd := treeutils.GenerateAction("send", "send an email",
		"send an email to the given recipient via the configured mail server ("+stylesheet.Cur.Action.Render("show")+").\n"+
			"The email will be sent via the webserver, not this machine.\n"+
			"Attachments are not currently supported.", nil,
		func(c *cobra.Command, s []string) error {
			// check that a mail configuration is set
			if cfg, err := connection.Client.MailConfig(); err != nil {
				return err
			} else if cfg.Server == "" || cfg.Port == 0 {
				return errors.New("You must configure a mail server before you can send mail")
			}

			from, to, subject, body, err := getFlags(c.Flags())
			if err != nil {
				return err
			}
			if from == "" || len(to) == 0 || body == "" {
				x, err := c.Flags().GetBool(ft.NoInteractive.Name())
				if err != nil {
					clilog.GetFlag(err)
					x = true
				}
				if x {
					return errors.New("--from, --to, and --body are required")
				}
				return mother.Spawn(c.Root(), c, s)
			}
			err = connection.Client.SendMail(from, to, subject, body, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Queued mail %s to %v\n", subject, to)
			return nil
		},
		treeutils.GenerateActionOptions{})
	cmd.Flags().AddFlagSet(flags())

	return action.NewPair(cmd, newSendMailModel())
}

func flags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.String("from", "", "who is this email from? If omitted, defaults to your user's email address")
	fs.StringSlice("to", nil, "who is the intended recipient?")
	fs.String("subject", "", "subject of the email")
	fs.String("body", "", "contents of the email")
	return fs
}

func getFlags(fs *pflag.FlagSet) (from string, to []string, subject, body string, err error) {
	var errOccurred bool
	from, err = fs.GetString("from")
	if err != nil {
		clilog.GetFlag(err)
		errOccurred = true
	} else if from == "" {
		from = connection.CurrentUser().Email
	}
	to, err = fs.GetStringSlice("to")
	if err != nil {
		clilog.GetFlag(err)
		errOccurred = true
	}
	subject, err = fs.GetString("subject")
	if err != nil {
		clilog.GetFlag(err)
		errOccurred = true
	}
	body, err = fs.GetString("body")
	if err != nil {
		clilog.GetFlag(err)
		errOccurred = true
	}

	if errOccurred { // ensure err is sent if any occurred
		err = clilog.ErrInternal{}
	}
	return from, to, subject, body, err
}

type selected uint

const (
	from selected = iota
	to
	subject
	body
	submit
)

type model struct {
	selected selected

	fromTI    textinput.Model
	toTA      textarea.Model
	subjectTI textinput.Model
	bodyTA    textarea.Model

	done bool
}

func newSendMailModel() *model {
	m := &model{}
	m.Reset()
	return m
}

func (m *model) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	// check that a mail configuration is set
	if cfg, err := connection.Client.MailConfig(); err != nil {
		return "", nil, err
	} else if cfg.Server == "" || cfg.Port == 0 {
		return "You must configure a mail server before you can send mail", nil, nil
	}

	fs := flags()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	from, to, subject, body, err := getFlags(fs)
	if err != nil {
		return "", nil, err
	}
	// if everything was provided, we can immediately send
	if from != "" && len(to) > 0 && body != "" {
		m.done = true

		if err := connection.Client.SendMail(from, to, subject, body, nil); err != nil {
			return "", nil, err
		}
		return "", tea.Printf("Queued mail %s to %v", subject, to), nil
	}
	// pre-pop
	m.fromTI.SetValue(from)
	m.toTA.SetValue(strings.Join(to, "\n"))
	m.subjectTI.SetValue(subject)
	m.bodyTA.SetValue(body)

	return "", nil, nil
}

func (m *model) Update(msg tea.Msg) tea.Cmd {
	if hotkeys.Match(msg, hotkeys.CursorDown) {
		m.next()
		return textinput.Blink
	} else if hotkeys.Match(msg, hotkeys.CursorUp) {
		m.prev()
		return textinput.Blink
	} else if hotkeys.ButtonPressed(msg) && m.selected == submit {
		// if any fields have error, do nothing
		if m.fromTI.Err != nil || m.toTA.Err != nil || m.subjectTI.Err != nil || m.bodyTA.Err != nil {
			return nil
		}
		var to []string
		for line := range strings.FieldsSeq(m.toTA.Value()) {
			to = append(to, strings.TrimSpace(line))
		}

		err := connection.Client.SendMail(m.fromTI.Value(), to, m.subjectTI.Value(), m.bodyTA.Value(), nil)
		m.done = true
		if err != nil {
			return tea.Println(err)
		}
		return tea.Printf("Queued mail %s to %v", m.subjectTI.Value(), to)
	}
	// pass to the appropriate update
	var cmd tea.Cmd
	switch m.selected {
	case from:
		m.fromTI, cmd = m.fromTI.Update(msg)
	case to:
		m.toTA.Err = nil
		m.toTA, cmd = m.toTA.Update(msg)
		// check each line item; fail on first error
		for line := range strings.FieldsSeq(m.toTA.Value()) {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if err := validate.Email(line); err != nil {
				m.toTA.Err = err
				break
			}
		}
	case subject:
		m.subjectTI, cmd = m.subjectTI.Update(msg)
	case body:
		m.bodyTA, cmd = m.bodyTA.Update(msg)
	}

	return cmd
}

func (m *model) next() {
	switch m.selected {
	case from:
		m.fromTI.Blur()
	case to:
		m.toTA.Blur()
	case subject:
		m.subjectTI.Blur()
	case body:
		m.bodyTA.Blur()
	}

	if m.selected == submit {
		m.selected = 0
	} else {
		m.selected += 1
	}

	switch m.selected {
	case from:
		m.fromTI.Focus()
	case to:
		m.toTA.Focus()
	case subject:
		m.subjectTI.Focus()
	case body:
		m.bodyTA.Focus()
	}
}

func (m *model) prev() {
	switch m.selected {
	case from:
		m.fromTI.Blur()
	case to:
		m.toTA.Blur()
	case subject:
		m.subjectTI.Blur()
	case body:
		m.bodyTA.Blur()
	}

	if m.selected == 0 {
		m.selected = submit
	} else {
		m.selected -= 1
	}

	switch m.selected {
	case from:
		m.fromTI.Focus()
	case to:
		m.toTA.Focus()
	case subject:
		m.subjectTI.Focus()
	case body:
		m.bodyTA.Focus()
	}
}

func (m *model) View() string {
	var sb strings.Builder

	sb.WriteString(stylesheet.Pip(uint(m.selected), uint(from)) + stylesheet.Cur.FieldText.Render("From:") + " ")
	sb.WriteString(m.fromTI.View() + "\n")

	sb.WriteString(stylesheet.Pip(uint(m.selected), uint(to)) + stylesheet.Cur.FieldText.Render("To:") + "\n")
	sb.WriteString(m.toTA.View() + "\n")

	sb.WriteString(stylesheet.Pip(uint(m.selected), uint(subject)) + stylesheet.Cur.FieldText.Render("Subject:") + " ")
	sb.WriteString(m.subjectTI.View() + "\n")

	sb.WriteString(stylesheet.Pip(uint(m.selected), uint(body)) + stylesheet.Cur.FieldText.Render("Body:") + "\n")
	sb.WriteString(m.bodyTA.View() + "\n")

	var errors []string
	if m.fromTI.Err != nil {
		errors = append(errors, m.fromTI.Err.Error())
	}
	if m.toTA.Err != nil {
		errors = append(errors, m.toTA.Err.Error())
	}
	if m.subjectTI.Err != nil {
		errors = append(errors, m.subjectTI.Err.Error())
	}
	if m.bodyTA.Err != nil {
		errors = append(errors, m.bodyTA.Err.Error())
	}

	sb.WriteString("\n" + stylesheet.ViewSubmitButton(m.selected == submit, m.bodyTA.Width(), errors...))

	return sb.String()
}

func (m *model) Done() bool {
	return m.done
}

func (m *model) Reset() error {
	m.selected = from

	m.fromTI = stylesheet.NewTI("", false)
	m.fromTI.Validate = validate.Email
	m.toTA = textarea.New()
	m.subjectTI = stylesheet.NewTI("", true)
	m.bodyTA = textarea.New()

	m.done = false
	return nil
}
