/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package mother is the heart and brain of the interactive functionality of gwcli.
It is the top-level implementation of tea.Model and drives interactive tree navigation as well as
managing of child processing (Actions).

Almost all interactivity flows through Mother, even when a child is in control (aka: Mother is in
handoff mode); Mother's Update() and View() are still called every cycle, but control rapidly passes
to the child's Update() and View().

Mother also manages the top-level prompt.
*/
package mother

import (
	"fmt"
	"strings"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/mother/traverse"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/killer"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/shlex"
	"github.com/gravwell/gravwell/v4/ingest/log"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type navCmd = cobra.Command
type actionCmd = cobra.Command // actions have associated actors via the action map

// Mother is a struct satisfying the tea.Model interface and containing information required for cobra.Command tree traversal.
//
// Serves as the beating heart of interactive gwcli.
//
// Mother is considered a singleton (specifically, the app itself) and should not be passed around except by BubbleTea.
type Mother struct {
	mode mode

	// tree references
	root *navCmd
	pwd  *navCmd

	// prompt
	ti          textinput.Model
	suggestions struct {
		nav    []traverse.Suggestion
		action []traverse.Suggestion
		bi     []traverse.Suggestion
		tab    string
	}

	// terminal information
	width  int
	height int

	active struct {
		command *actionCmd   // command user called
		model   action.Model // Elm Arch associated to command
	}

	processOnStartup bool // mother should immediately consume and process her prompt on spawn
	dieOnChildDone   bool // sister to processOnStartup; causes Mother to quit when child completes
	exiting          bool // if true, we have already issued a tea.Quit and are just waiting for it to process; take no further action

	history *history
}

// Spawn spins up a new instance of Mother in a fresh tea program, runs the
// program, and returns on Mother's exit.
// The caller is expected to exit on Spawn's return.
func Spawn(root, cur *cobra.Command, trailingTokens []string) error {
	// spin up mother
	interactive := tea.NewProgram(New(root, cur, trailingTokens, nil))
	// reactive the admin command
	if c, _, err := root.Find([]string{"user", "admin"}); err != nil {
		clilog.Writer.Warnf("failed to reveal the admin command")
	} else if c != nil {
		c.Hidden = false
	}

	if _, err := interactive.Run(); err != nil {
		panic(err)
	}
	return interactive.ReleaseTerminal() // should be redundant
}

// New spawns a new Mother instance on the root tree.
// Returns the new instance, which can be fed into bubble tea as a model.
//
// cur must be a child of root or you'll get some really weird traversal.
//
// NOTE: trailingTokens is not currently used, but is included for flexibility, in case it needs to be built into the startupCommand.
//
// Renderer is only to be used for tests; it should be left nil otherwise.
func New(root *navCmd, cur *cobra.Command, trailingTokens []string, _ *lipgloss.Renderer) Mother {
	// spin up builtins
	initBuiltins()

	// disable completions command when mother is spun up
	if c, _, err := root.Find([]string{"completion"}); err != nil {
		clilog.Writer.Warnf("failed to disable 'completion' command: %v", err)
	} else if c != nil {
		root.RemoveCommand(c)
	}

	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { f.Hidden = true })

	// text input
	ti := textinput.New()
	// disable the placeholder and in-line suggestions if no-color is active
	if !stylesheet.NoColor {
		ti.Placeholder = "help"
		ti.ShowSuggestions = true
	}
	ti.Prompt = "" // replicated externally
	ti.Focus()
	ti.Width = stylesheet.TIWidth // replaced on first WindowSizeMsg, proc'd by Init()
	// add ctrl+left/right to the word traversal keys
	ti.KeyMap.WordForward.SetKeys("ctrl+right", "alt+right", "alt+f")
	ti.KeyMap.WordBackward.SetKeys("ctrl+left", "alt+left", "alt+b")

	m := Mother{
		root:    root,
		pwd:     cur,
		mode:    prompting,
		ti:      ti,
		history: newHistory()}
	// set mother's starting position
	if cur == nil {
		m.pwd = root // place mother at root
	} else if cur.GroupID == group.ActionID { // special handling for action starts
		m.pwd = cur.Parent() // place mother at the action's parent

		// rebuild the expected action and flags on mother's prompt
		var p strings.Builder
		p.WriteString(cur.Name())
		cur.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Changed {
				p.WriteString(fmt.Sprintf(" --%v=\"%v\"", f.Name, f.Value))
			}
		})
		m.ti.SetValue(p.String())

		// have mother immediate act on the data we placed on her prompt
		m.processOnStartup = true
	}

	clilog.Writer.Debugf("Spawning mother rooted @ %v, located @ %v, with trailing tokens %v",
		m.root.Name(), m.pwd.Name(), trailingTokens)

	return m
}

//#region tea.Model implementation

var _ tea.Model = Mother{}

func (m Mother) Init() tea.Cmd {
	return tea.WindowSize() // TODO we can likely junk this
}

// Update (specifically Mother's Update()) is always the entrypoint for BubbleTea to drive.
// It checks for kill keys (to disallow a runaway/ill-designed child), then either passes off
// control (if in handoff mode) or handles the input itself (if in prompt mode).
func (m Mother) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.exiting {
		return m, nil
	}
	if m.processOnStartup {
		m.processOnStartup = false
		m.dieOnChildDone = true
		return m, processInput(&m)
	}
	switch killer.CheckKillKeys(msg) { // handle kill keys above all else
	case killer.Global:
		// if in handoff mode, just kill the child
		if m.mode == handoff {
			clilog.Writer.Infof("Global killing %v. Reasserting...", m.active.command.Name())
			m.unsetAction()
			// if we are killing from mother, we must manually exit alt screen
			// (harmless if not in use)
			return m, tea.Batch(tea.ExitAltScreen, textinput.Blink)
		}
		m.exiting = true
		connection.End()
		return m, tea.Sequence(tea.Println("Bye"), tea.Quit)
	case killer.Child: // ineffectual if not in handoff mode
		if m.mode == handoff { // to prevent segfault, as active is nil
			clilog.Writer.Infof("Child killing %v. Reasserting...", m.active.command.Name())
		}
		m.unsetAction()
		return m, tea.Batch(tea.ExitAltScreen, textinput.Blink)
	}

	if m.mode == handoff { // a child is running
		if clilog.Active(clilog.DEBUG) {
			activeChildSanityCheck(m)
		}
		// test for child state
		if !m.active.model.Done() { // child still processing
			return m, m.active.model.Update(msg)
		} else {
			// child has finished processing, regain control and return to normal processing
			clilog.Writer.Infof("%v done. Reasserting...", m.active.command.Name())
			m.unsetAction()
			return m, textinput.Blink
		}
	}

	// if we booted directly into an action, die now that it is done
	if m.dieOnChildDone {
		m.exiting = true
		connection.End()
		return m, tea.Sequence(tea.Println("Bye"), tea.Quit)
	}

	// normal handling
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// save off terminal dimensions
		m.width = msg.Width
		m.height = msg.Height
		// update mother's prompt width
		m.ti.Width = msg.Width -
			lipgloss.Width(m.pwd.CommandPath()) - // reserve space for prompt head
			3 // include a padding
	case tea.KeyMsg:
		// NOTE kill keys are handled above
		switch msg.Type {
		case tea.KeyF1: // help
			return m, contextHelp(&m, m.pwd, strings.Split(strings.TrimSpace(m.ti.Value()), " "))
		case tea.KeyUp: // history
			m.ti.SetValue(m.history.getOlderRecord())
			// update cursor position
			m.ti.CursorEnd()
		case tea.KeyDown: // history
			m.ti.SetValue(m.history.getNewerRecord())
			// update cursor position
			m.ti.CursorEnd()
		case tea.KeyEnter:
			m.history.unsetFetch()
			return m, processInput(&m)
		case tea.KeyCtrlL:
			return m, clear(&m, nil, nil)
		case tea.KeyTab:
			if m.ti.Value() == "" {
				m.ti.SetValue("help")
			}
		case tea.KeyCtrlU:
			m.ti.SetValue("")
		}
	}

	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	m.regenerateSuggestions(m.ti.Value())
	return m, cmd
}

// regenerateSuggestions parses the user input to set next-word suggestions for navs, actions, and builtins.
// The first suggestion (highest to lowest priority: navs, actions, builtins) is also set as a possible tab-complete on the prompt.
func (m *Mother) regenerateSuggestions(userInput string) {
	m.suggestions.tab = "" // don't allow old suggestions to linger
	m.suggestions.nav, m.suggestions.action, m.suggestions.bi = traverse.DeriveSuggestions(m.ti.Value(), m.pwd, builtinKeys)
	if len(m.suggestions.nav) > 0 {
		m.suggestions.tab = userInput + m.suggestions.nav[0].FullName[len(m.suggestions.nav[0].MatchedCharacters):]
	} else if len(m.suggestions.action) > 0 {
		m.suggestions.tab = userInput + m.suggestions.action[0].FullName[len(m.suggestions.action[0].MatchedCharacters):]
	} else if len(m.suggestions.bi) > 0 {
		m.suggestions.tab = userInput + m.suggestions.bi[0].FullName[len(m.suggestions.bi[0].MatchedCharacters):]
	}
	m.ti.SetSuggestions([]string{m.suggestions.tab})
}

// helper function for m.Update.
// Validates that mother's active states have not become corrupted by a bug elsewhere in the code.
// Panics if it detects an error
func activeChildSanityCheck(m Mother) {
	if m.active.model == nil || m.active.command == nil {
		clilog.Writer.Warnf(
			"Mother is in handoff mode but has inconsistent actives %#v",
			m.active)
		if m.active.command == nil {
			clilog.Writer.Warnf("nil command, unable to recover. Dying...")
			panic("inconsistent handoff mode. Please submit a bug report.")
		}
		// m.active.model == nil, !m.active.command
		var err error
		m.active.model, err = action.GetModel(m.active.command)
		if err != nil {
			clilog.Writer.Errorf("failed to recover model from command: %v", err)
			panic("inconsistent handoff mode. Please submit a bug report. ")
		}
	}
}

// View either passes off control to the active child's .View() or compiles a prompt and set of suggestions.
//
// The prompt displays the user's current text and a list of matching suggestions up to the next space.
// For example: `>syst` will suggest `systems` while `>system i` will suggest `systems indexers` and `system ingesters`.
func (m Mother) View() string {
	// check short-circuits
	if m.exiting { // don't bother to draw
		return ""
	} else if m.active.model != nil { // allow child command to retain control, if it exists
		return m.active.model.View()
	} else if m.dieOnChildDone { // don't bother to draw
		return ""
	}

	// format current suggestions
	var (
		sb         strings.Builder // using a string builder to reduce allocation
		ns, as, bs string
	)
	for _, suggestion := range m.suggestions.nav {
		sb.WriteString(stylesheet.Cur.Nav.Render(suggestion.MatchedCharacters) + suggestion.FullName[len(suggestion.MatchedCharacters):] + " ")
	}
	ns = strings.TrimSpace(sb.String()) // chip last space
	sb.Reset()
	for _, suggestion := range m.suggestions.action {
		sb.WriteString(stylesheet.Cur.Action.Render(suggestion.MatchedCharacters) + suggestion.FullName[len(suggestion.MatchedCharacters):] + " ")
	}
	as = strings.TrimSpace(sb.String()) // chip last space
	sb.Reset()
	for _, suggestion := range m.suggestions.bi {
		sb.WriteString(stylesheet.Cur.TertiaryText.Render(suggestion.MatchedCharacters) + suggestion.FullName[len(suggestion.MatchedCharacters):] + " ")
	}
	bs = strings.TrimSpace(sb.String()) // chip last space

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		m.promptString(true), ns, as, bs)
}

//#endregion

// processInput consumes and clears the text on the prompt, determines what action to take, modifies
// the model accordingly, and outputs the state of the prompt as a newline.
// ! Be sure each path that clears the prompt also outputs it via tea.Println
func processInput(m *Mother) tea.Cmd {
	// sanity check error state of the ti
	if m.ti.Err != nil {
		clilog.Writer.Warnf("text input has a reported error: %v", m.ti.Err)
		m.ti.Err = nil
	}

	var (
		historyCmd tea.Cmd
		input      string
		err        error
	)
	if historyCmd, input, err = m.pushToHistory(); err != nil {
		clilog.Writer.Warnf("pushToHistory returned %v", err)
		return nil
	}

	wr, err := traverse.Walk(m.pwd, input, builtinKeys)
	if err != nil {
		return tea.Sequence(
			historyCmd,
			tea.Println(stylesheet.Cur.ErrorText.Render(err.Error())),
		)
	}
	if wr.HelpMode {
		return tea.Sequence(
			historyCmd,
			contextHelp(m, wr.EndCmd, []string{wr.Builtin}),
		)
	}
	// invoke action, nav, or builtin
	if wr.Builtin != "" {
		return tea.Sequence(
			historyCmd,
			builtins[wr.Builtin](m, wr.EndCmd, wr.RemainingTokens),
		)
	} else if wr.EndCmd != nil {
		if action.Is(wr.EndCmd) {
			cmd := processActionHandoff(m, wr.EndCmd, strings.Join(wr.RemainingTokens, " "))
			if cmd == nil {
				return historyCmd
			}
			return tea.Sequence(historyCmd, cmd)
		}
		// move mother to target nav
		m.pwd = wr.EndCmd
		return historyCmd
	}

	// if we made it this far, err, builtin, and endcmd are all nil so we have nothing to act on.
	// this probably means input was nil, so warn if it wasn't
	if input == "" {
		clilog.Writer.Warn("taking no action on process input", rfc5424.SDParam{Name: "input", Value: input})
	}

	return historyCmd
}

// pushToHistory generates and stores historical record of the prompt (as a
// Println and in the history array) and then clears the prompt, returning
// cleaned, usable user input
func (m *Mother) pushToHistory() (println tea.Cmd, userIn string, err error) {
	userIn = m.ti.Value()
	if m.ti.Err != nil {
		return nil, userIn, m.ti.Err
	}
	p := m.promptString(false)

	m.history.insert(userIn)           // add prompt string to history
	m.ti.Reset()                       // empty out the input
	return tea.Println(p), userIn, nil // print prompt
}

// Composes the gwcli prompt as a single line.
// If live, uses m.ti.View() (thus displaying the blinking cursor).
// If !live, uses m.ti.Value() (for history use)
func (m *Mother) promptString(live bool) string {
	var ti string
	if live {
		ti = m.ti.View()
	} else {
		ti = m.ti.Value()
	}
	return stylesheet.Cur.Prompt(m.pwd.CommandPath(), connection.AdminMode()) + ti
}

// helper subroutine for processInput
//
// Prepares mother and the named action for handoff, undoing itself if an error occurs.
//
// Returns commands to run after the push-to-history command.
// These commands are either commands the action wants run to setup or an error print if an error
// occurred
func processActionHandoff(m *Mother, actionCmd *cobra.Command, remString string) tea.Cmd {
	m.mode = handoff

	// split remaining tokens
	args, err := shlex.Split(remString)
	if err != nil {
		clilog.Writer.Errorf("failed to split remaining string %v: %v", remString, err)
	}

	// look up the subroutines to load
	m.active.model, _ = action.GetModel(actionCmd) // save add-on subroutines
	if m.active.model == nil {                     // undo and return
		m.unsetAction()
		str := fmt.Sprintf("Did not find actor associated to '%s'.", actionCmd.Name())
		clilog.Writer.Warnf(str+" %#v", actionCmd)
		return tea.Printf("Developer error: %v. Please submit a bug report.\n", str)
	}
	m.active.command = actionCmd

	// don't bother visiting if it won't be printed
	if clilog.Writer.GetLevel() == log.DEBUG {
		var fStr strings.Builder
		m.active.command.InheritedFlags().Visit(func(f *pflag.Flag) {
			fStr.WriteString(fmt.Sprintf("%s - %s", f.Name, f.Value))
		})
		clilog.Writer.Debugf("Passing args (%v) and inherited flags (%#v) into %s\n",
			remString,
			fStr.String(),
			m.active.command.Name())
	}

	// NOTE: the inherited flags here may have a combination of parsed and !parsed flags
	// persistent commands defined below root may not be parsed

	var (
		invalid string
		cmd     tea.Cmd
	)
	if invalid, cmd, err = m.active.model.SetArgs(m.active.command.InheritedFlags(), args, m.width, m.height); err != nil || invalid != "" { // undo and return
		m.unsetAction()

		if err != nil {
			errString := fmt.Sprintf("Failed to set args %v: %v", remString, err)
			clilog.Writer.Errorf("%v\nactive model %v\nactive command%v",
				errString, m.active.model, remString)
			return tea.Println(errString)
		}
		return tea.Println("invalid arguments: " + invalid + "\n" +
			"See " + stylesheet.Cur.ExampleText.Render("help") + " (or append -h) for assistance.")
	}
	clilog.Writer.Debugf("Handing off control to %s", m.active.command.Name())
	if cmd != nil {
		return cmd
	}
	return nil
}

// helper subroutine for updateSuggestions().
// Recursively searches down the given nav, returning all actions (at any depth), rooted at the
// given nav.
//
// Drives the suggestions of mother's prompt.
//
// Very similar to the tree action at root.
func plumbCommand(nav *navCmd) []string {
	self := nav.Name()
	var suggests = []string{self}
	for _, child := range nav.Commands() {
		switch child.GroupID {
		case group.NavID:
			subchildren := plumbCommand(child)
			for _, sc := range subchildren {
				suggests = append(suggests, self+" "+sc)
			}
		default: // actions end here
			suggests = append(suggests, self+" "+child.Name())
		}
	}
	return suggests
}

// unsetAction resets the current active command/action, clears actives, and returns control to
// Mother.
func (m *Mother) unsetAction() {
	if m.active.model != nil {
		m.active.model.Reset()
	}

	m.mode = prompting
	m.active.model = nil
	m.active.command = nil
}

//#region static helper functions

// TeaCmdContextHelp returns a tea.Println Cmd containing the context help for the given command.
//
// Structure:
//
// <nav> - <desc>
//
// --> <childnav> <childaction> <childnav>
//
// <nav> - <desc>
//
// --> <childaction>
//
// <action> - <desc>
func TeaCmdContextHelp(c *cobra.Command) tea.Cmd {
	// generate a list of all available Navs and Actions with their associated shorts
	var s strings.Builder

	if action.Is(c) {
		// redirect output to capture help
		priorOut := c.OutOrStdout()
		var sb strings.Builder
		c.SetOut(&sb)
		if err := c.Help(); err != nil {
			clilog.Writer.Warnf("failed to get help for command %v", c.CommandPath())
		}

		s.WriteString(sb.String())
		c.SetOut(priorOut)
	} else {
		specialStyle := stylesheet.Cur.SecondaryText
		// write .. and / if we are below root
		if c.HasParent() {
			fmt.Fprintf(&s, "%s%s - %s\n",
				stylesheet.Indent, specialStyle.Render(traverse.UpToken), "step up")
			fmt.Fprintf(&s, "%s%s - %s\n",
				stylesheet.Indent, specialStyle.Render(traverse.RootToken), "return to root")
		}
		children := c.Commands()
		for _, child := range children {
			// handle special commands
			if child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			var name string
			var subchildren strings.Builder // children of this child
			if action.Is(child) {
				name = stylesheet.Cur.Action.Render(child.Name())
			} else {
				name = stylesheet.Cur.Nav.Render(child.Name())
				// build and color subchildren
				for _, sc := range child.Commands() {
					_, err := subchildren.WriteString(stylesheet.ColorCommandName(sc) + " ")
					if err != nil {
						clilog.Writer.Warnf("Failed to generate list of subchildren: %v", err)
					}
				}

			}
			// generate the output
			trimmedSubChildren := strings.TrimSpace(subchildren.String())
			s.WriteString(fmt.Sprintf("%s%s - %s\n", stylesheet.Indent, name, child.Short))
			if trimmedSubChildren != "" {
				s.WriteString(stylesheet.Indent + stylesheet.Indent + trimmedSubChildren + "\n")
			}
		}
	}

	// write help footer
	s.WriteString("\nTry " + stylesheet.Cur.ExampleText.Render("help help") +
		" for information on using the help command.")

	// chomp last newline and return
	return tea.Println(strings.TrimSuffix(s.String(), "\n"))
}

//#endregion
