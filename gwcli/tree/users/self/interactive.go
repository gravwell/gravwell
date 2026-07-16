package self

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//#region change-password

type selfCPStage uint

const (
	selfCPStgCurrentPass selfCPStage = iota
	selfCPStgNewPass
	selfCPStgDone
)

func changePassword() action.Pair {
	cmd := treeutils.GenerateAction("change-password", "change your password",
		"Change the password for your account.",
		func(c *cobra.Command, args []string) error {
			currentPass, err := c.Flags().GetString("current-password")
			if err != nil {
				return clilog.GetFlag(err)
			}
			newPass, err := c.Flags().GetString("new-password")
			if err != nil {
				return clilog.GetFlag(err)
			}

			if currentPass != "" && newPass != "" {
				uid := connection.CurrentUser().ID
				if err := connection.Client.UserChangePass(uid, currentPass, newPass); err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), "password changed successfully")
				return nil
			}

			if state.Interactive() {
				return mother.Spawn(c.Root(), c, args)
			}
			if currentPass == "" {
				return fmt.Errorf("--current-password must be non-empty")
			}
			return fmt.Errorf("--new-password must be non-empty")
		},
	)
	cmd.Flags().String("current-password", "", "your current password")
	cmd.Flags().String("new-password", "", "your new password")

	return action.NewPair(cmd, &selfChangePassModel{})
}

type selfChangePassModel struct {
	currentTI textinput.Model
	newTI     textinput.Model
	stage     selfCPStage
}

func (m *selfChangePassModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	switch m.stage {
	case selfCPStgCurrentPass:
		if hotkeys.Match(msg, hotkeys.Invoke) {
			if m.currentTI.Value() == "" {
				return nil
			}
			m.stage = selfCPStgNewPass
			m.currentTI.Blur()
			m.newTI.Focus()
			return textinput.Blink
		}
		m.currentTI, cmd = m.currentTI.Update(msg)
	case selfCPStgNewPass:
		if hotkeys.Match(msg, hotkeys.Invoke) {
			if m.newTI.Value() == "" {
				return nil
			}
			uid := connection.CurrentUser().ID
			if err := connection.Client.UserChangePass(uid, m.currentTI.Value(), m.newTI.Value()); err != nil {
				m.stage = selfCPStgDone
				return tea.Printf("failed to change password: %v", err)
			}
			m.stage = selfCPStgDone
			return tea.Println("password changed successfully")
		}
		m.newTI, cmd = m.newTI.Update(msg)
	}
	return cmd
}

func (m *selfChangePassModel) View() string {
	switch m.stage {
	case selfCPStgCurrentPass:
		return fmt.Sprintf("Current password:\n%s\n\n  %s",
			m.currentTI.View(),
			stylesheet.Cur.DisabledText.Render("↲ continue • esc cancel"))
	case selfCPStgNewPass:
		return fmt.Sprintf("Current password: %s\nNew password:\n%s\n\n  %s",
			stylesheet.Cur.DisabledText.Render("(entered)"),
			m.newTI.View(),
			stylesheet.Cur.DisabledText.Render("↲ submit • esc cancel"))
	}
	return ""
}

func (m *selfChangePassModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	m.currentTI = stylesheet.NewTI("", false)
	m.currentTI.EchoMode = textinput.EchoPassword
	m.currentTI.Placeholder = "current password"
	m.currentTI.Width = 40
	m.currentTI.Focus()

	m.newTI = stylesheet.NewTI("", false)
	m.newTI.EchoMode = textinput.EchoPassword
	m.newTI.Placeholder = "new password"
	m.newTI.Width = 40
	m.newTI.Blur()

	return "", textinput.Blink, nil
}

func (m *selfChangePassModel) Done() bool { return m.stage == selfCPStgDone }

func (m *selfChangePassModel) Reset() error {
	m.currentTI = textinput.Model{}
	m.newTI = textinput.Model{}
	m.stage = selfCPStgCurrentPass
	return nil
}

//#region search-group

func searchGroup() action.Pair {
	cmd := treeutils.GenerateAction("search-groups", "get or set default search groups",
		"Display or update the default search groups for your account.\n",
		func(c *cobra.Command, args []string) error {
			set, clear, err := getSearchGroupsFlags(c.Flags())
			if err != nil {
				return err
			}
			success, bootInteractive, err := handleNonInteractive(set, clear, c.Flags().Args())
			if err != nil {
				if bootInteractive { // try to boot interactive mode instead of printing the error
					if !state.Interactive() { // no dice
						return err
					}
					return mother.Spawn(c.Root(), c, args)
				}
				return err
			}

			fmt.Fprintln(c.OutOrStdout(), success)
			return nil
		}, treeutils.GenerateActionOptions{
			Usage: ft.MutuallyExclusive(ft.Optional("--set"), ft.Optional("--clear")) +
				" " + ft.VariadicArgs("GID", false)},
	)
	cmd.Flags().AddFlagSet(searchGroupsFlags())
	cmd.Aliases = []string{"search-group", "sg"}
	return action.NewPair(cmd, &searchGroupModel{})
}

type searchGroupModel struct {
	doneInSetArgs bool
	m             multiselectlist.Model[int32]
}

func searchGroupsFlags() *pflag.FlagSet {
	fs := &pflag.FlagSet{}
	fs.Bool("set", false, "read the list of bare arguments as a list of group IDs to set as default."+
		" If no bare arguments are given and --set, a list of groups will be displayed so they can be selected interactively."+
		" Mutually exclusive with --clear")
	fs.Bool("clear", false, "clear your search groups. Mutually exclusive with --set.")
	return fs
}

func getSearchGroupsFlags(fs *pflag.FlagSet) (set, clear bool, err error) {
	set, err = fs.GetBool("set")
	if err != nil {
		clilog.GetFlag(err)
	}
	clear, err = fs.GetBool("clear")
	if err != nil {
		clilog.GetFlag(err)
	}
	if set && clear {
		return false, false, ft.ErrMutuallyExclusive("set", "clear")
	}
	return set, clear, nil
}

// May return bootInteractive and err simultaneously; this means to return the error to the user iff -x.
func handleNonInteractive(set, clear bool, bareArgs []string) (success string, bootInteractive bool, err error) {
	if len(bareArgs) > 0 && !set { // sanity check
		return "", false, errors.New("you may only pass group IDs when --set is set. No action taken.")
	} else if len(bareArgs) == 0 && set { // attempt to boot interactive
		return "", true, errors.New("--set requires group IDs be passed as bare arguments")
	} else if len(bareArgs) > 0 && set { // set non-interactively
		var gids []int32
		for _, arg := range bareArgs {
			arg = strings.TrimSpace(arg)
			if arg == "" {
				continue
			}
			gid, err := strconv.ParseInt(arg, 10, 32)
			if err != nil {
				return "", false, fmt.Errorf("%s is not a valid group ID", arg)
			}
			gids = append(gids, int32(gid))
		}
		if err := connection.Client.SetDefaultSearchGroups(connection.CurrentUser().ID, gids); err != nil {
			return "", false, err
		}
		return fmt.Sprintf("user %d search groups updated to %v\n", connection.CurrentUser().ID, gids), false, nil
	} else if clear {
		if err := connection.Client.DeleteDefaultSearchGroups(connection.CurrentUser().ID); err != nil {
			return "", false, err
		}
		return "search groups cleared", false, nil
	}
	// get
	gids, err := connection.Client.GetDefaultSearchGroups(connection.CurrentUser().ID)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%v\n", gids), false, nil
}

func (c *searchGroupModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	fs := searchGroupsFlags()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	set, clear, err := getSearchGroupsFlags(fs)
	if err != nil {
		return "", nil, err
	}
	success, bootInteractive, err := handleNonInteractive(set, clear, fs.Args())
	if bootInteractive { // continue into interactive mode
		grps, err := connection.Client.Groups()
		if err != nil {
			clilog.Writer.Error("failed to get groups", log.KV("error", err))
			return "", nil, fmt.Errorf("failed to get groups")
		}
		if len(grps) == 0 {
			return "you are not a member of any groups", nil, nil
		}

		// get current defaults to pre-select
		uid := connection.CurrentUser().ID
		currentGIDs, err := connection.Client.GetDefaultSearchGroups(uid)
		if err != nil {
			clilog.Writer.Warn("failed to get current default search groups", log.KVErr(err))
		}
		currentSet := make(map[int32]bool, len(currentGIDs))
		for _, gid := range currentGIDs {
			currentSet[gid] = true
		}

		var itms = make([]multiselectlist.SelectableItem[int32], 0, len(grps))
		for _, grp := range grps {
			itms = append(itms, &multiselectlist.DefaultSelectableItem[int32]{
				Title_:       grp.Name,
				Description_: fmt.Sprintf("(ID: %d) %s", grp.ID, grp.Description),
				Selected_:    currentSet[grp.ID],
				ID_:          grp.ID,
			})
		}
		itms = slices.Clip(itms)

		c.m = multiselectlist.New(itms, width, height, multiselectlist.Options{})
		c.m.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.m.StatusMessageOnSelect = true
		c.m.Title = "Select default search groups (↲ with none selected to clear)"
		return "", nil, nil
	} else if err != nil {
		return "", nil, err
	}
	c.doneInSetArgs = true
	return "", tea.Println(success), nil
}

func (c *searchGroupModel) Update(msg tea.Msg) (cmd tea.Cmd) {
	if c.doneInSetArgs {
		return nil
	}
	c.m, cmd = c.m.Update(msg)
	if c.m.Done() {
		uid := connection.CurrentUser().ID
		selected := c.m.GetSelectedItems()
		if len(selected) == 0 {
			// clear default groups
			if err := connection.Client.DeleteDefaultSearchGroups(uid); err != nil {
				return tea.Printf("failed to clear search groups: %v", err)
			}
			return tea.Println("search groups cleared")
		}
		var gids []int32
		for _, li := range selected {
			gids = append(gids, li.ID())
		}
		if err := connection.Client.SetDefaultSearchGroups(uid, gids); err != nil {
			return tea.Printf("failed to set search groups: %v", err)
		}
		return tea.Printf("search groups updated to: %v", gids)
	}
	return cmd
}

func (c *searchGroupModel) View() string {
	if c.doneInSetArgs {
		return ""
	}
	return c.m.View()
}
func (c *searchGroupModel) Done() bool {
	return c.doneInSetArgs || c.m.Done()
}
func (c *searchGroupModel) Reset() error {
	c.doneInSetArgs = false
	c.m = multiselectlist.Model[int32]{}
	return nil
}

//#region update

func update() action.Pair {
	return scaffoldcreate.NewCreateAction("user property updates",
		// no fields are required as empty fields are replaced by current values
		map[string]scaffoldcreate.Field{
			"username": {
				Title:    "Username",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-username",
					Usage: "new value for your username",
				},
				DefaultValue: "",
				Order:        100,
				Provider:     &scaffoldcreate.TextProvider{},
			},
			"name": {
				Title:    "Name",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-name",
					Usage: "new value for your name",
				},
				DefaultValue: "",
				Order:        90,
				Provider:     &scaffoldcreate.TextProvider{},
			},
			"email": {
				Title:    "Email",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{
					Name:  "new-email",
					Usage: "new value for your email",
				},
				DefaultValue: "",
				Order:        80,
				Provider:     &scaffoldcreate.TextProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// just to be safe, forcefully refresh our local info
			if err := connection.RefreshCurrentUser(); err != nil {
				return "", "", err
			}
			mi := connection.CurrentUser()
			var allEmpty = true
			// set empty information to whatever the current information on the server is
			username := strings.TrimSpace(fields["username"].Provider.Get())
			if username == "" {
				username = mi.Username
			} else {
				allEmpty = false
			}
			name := strings.TrimSpace(fields["name"].Provider.Get())
			if name == "" {
				name = mi.Name
			} else {
				allEmpty = false
			}
			email := strings.TrimSpace(fields["email"].Provider.Get())
			if email == "" {
				email = mi.Email
			} else {
				allEmpty = false
			}
			if allEmpty {
				return "", "", errors.New("no changes made")
			}
			defer connection.RefreshCurrentUser()

			if err := connection.Client.UpdateUserInfo(connection.CurrentUser().ID, username, name, email); err != nil {
				return "", "", err
			}
			return "", "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:   "update",
				Short: "Update your user information.",
				Long:  "Update the basic details of the current user. Empty values will remain unchanged.",
				Usage: fmt.Sprint("update ",
					ft.Optional("--new-username=<USERNAME>"), " ",
					ft.Optional("--new-name=<NAME>"), " ",
					ft.Optional("--new-email=<EMAIL>")),
			},
		})
}
