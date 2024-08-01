/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
A delete action consumes a list of delete-able items, allowing the user to select them
interactively or by passing a (numeric or UUID) ID.

Delete actions have the --dryrun and --id default flags.

Implementations will probably look a lot like:

	var aliases []string = []string{}

	func New[pkg]DeleteAction() action.Pair {
		return scaffolddelete.NewDeleteAction(aliases, [singular], [plural], del,
			func() ([]scaffold.Item[[integer]], error) {
				couldDelete, err := connection.Client.GetAll[X]()
				if err != nil {
					return nil, err
				}
				slices.SortFunc(couldDelete, func(m1, m2 types.[Y]) int {
					return strings.Compare(m1.Name, m2.Name)
				})
				var items = make([]scaffold.Item[[integer]], len(couldDelete))
				for i := range couldDelete {
					items[i] = scaffolddelete.New(couldDelete[i].Name,
						couldDelete[i].Description,
						couldDelete[i].ID)
				}
				return items, nil
			})
	}

	func del(dryrun bool, id uint64) error {
		if dryrun {
			_, err := connection.Client.Get[X](id)
			return err
		}
		return connection.Client.Delete[X](id)
	}
*/
package scaffolddelete

import (
	"fmt"
	"gwcli/action"
	"gwcli/clilog"
	"gwcli/mother"
	"gwcli/stylesheet"
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/listsupport"
	"gwcli/utilities/scaffold"
	"gwcli/utilities/treeutils"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v3/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A function that performs the (faux-, on dryrun) deletion once an item is picked
// only returns a value if the delete (or select, on dry run) failed
type deleteFunc[I scaffold.Id_t] func(dryrun bool, id I) error

// A function that fetches and formats the list of delete-able items.
// It must return an array of a struct that implements the Item interface.
type fetchFunc[I scaffold.Id_t] func() ([]Item[I], error)

// text to display when deletion is skipped due to error
const (
	errorNoDeleteText = "An error occured: %v.\nAbstained from deletion."
	dryrunSuccessText = "DRYRUN: %v (ID %v) would have been deleted"
	deleteSuccessText = "%v (ID %v) deleted"
)

const (
	confirmPhrase = "yes"
)

// NewDeleteAction creates and returns a cobra.Command suitable for use as a delete action.
// Base flags:
//
//	--dryrun (SELECT, as a mock deletion),
//
//	--id (immediately attempt deletion on the given id)
//
// You must provide two functions to instantiate a generic delete:
//
// Del is a function that performs the actual (mock) deletion.
// It is given the dryrun boolean and an ID value and returns an error only if the delete or select
// failed.
//
// Fch is a function that fetches all, delete-able records for the user to pick from.
// It returns a user-defined struct fitting the Item interface.
//
// dopts allows you to modify how each item is displayed in the list of delete-able items.
// While you could provide your own renderer via WithRender(), this is discouraged in order to
// maintain style uniformity.
func NewDeleteAction[I scaffold.Id_t](
	singular, plural string,
	del deleteFunc[I],
	fch fetchFunc[I]) action.Pair {
	cmd := treeutils.NewActionCommand(
		"delete",
		"delete a "+singular,
		"delete a "+singular+" by id or selection",
		[]string{},
		func(c *cobra.Command, s []string) {
			// fetch values from flags
			id, dryrun, err := fetchFlagValues[I](c.Flags())
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
				return
			}

			var zero I
			if id == zero {
				if script, err := c.Flags().GetBool("script"); err != nil {
					clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
					return
				} else if script {
					fmt.Fprintln(c.ErrOrStderr(), "--id is required in script mode")
					return
				}
				// spin up mother
				if err := mother.Spawn(c.Root(), c, s); err != nil {
					clilog.Tee(clilog.CRITICAL, c.ErrOrStderr(),
						"failed to spawn a mother instance: "+err.Error())
				}
				return

			}

			if err := del(dryrun, id); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error())
				return
			} else if dryrun {
				fmt.Fprintf(c.OutOrStdout(), dryrunSuccessText+"\n", singular, id)
			} else {
				fmt.Fprintf(c.OutOrStdout(), deleteSuccessText+"\n",
					singular, id)
			}
		})
	fs := flags()
	cmd.Flags().AddFlagSet(&fs)
	d := newDeleteModel(del, fch)
	d.itemSingular = singular
	d.itemPlural = plural
	return treeutils.GenerateAction(cmd, d)
}

// base flagset
func flags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.Name.Dryrun, false, ft.Usage.Dryrun)
	fs.String(ft.Name.ID, "", "ID of the item to be deleted")
	return fs
}

// helper function for getting and casting flag values
func fetchFlagValues[I scaffold.Id_t](fs *pflag.FlagSet) (id I, dryrun bool, _ error) {
	if strid, err := fs.GetString(ft.Name.ID); err != nil {
		return id, false, err
	} else if strid != "" {
		id, err = scaffold.FromString[I](strid)
		if err != nil {
			return id, dryrun, err
		}
	}
	if dr, err := fs.GetBool(ft.Name.Dryrun); err != nil {
		return id, dryrun, err
	} else {
		dryrun = dr
	}

	return
}

//#region interactive mode (model) implementation

type mode uint

const (
	selecting mode = iota
	quitting
	confirming
)

type deleteModel[I scaffold.Id_t] struct {
	width, height int

	itemSingular string        // "macro", "kit", "query"
	itemPlural   string        // "macros", "kits", "queries"
	mode         mode          // current mode
	flagset      pflag.FlagSet // parsed flag values (set in SetArgs)
	dryrun       bool
	df           deleteFunc[I] // function to delete an item
	ff           fetchFunc[I]  // function to get all delete-able items

	// selecting mode
	list list.Model

	// confirming mode
	selectedItem Item[I]
	confTI       textinput.Model
}

func newDeleteModel[I scaffold.Id_t](del deleteFunc[I], fch fetchFunc[I]) *deleteModel[I] {
	d := &deleteModel[I]{
		mode:   selecting,
		confTI: stylesheet.NewTI("", false),
	}
	d.flagset = flags()

	d.df = del
	d.ff = fch

	return d
}

func (d *deleteModel[I]) Update(msg tea.Msg) tea.Cmd {
	if d.Done() {
		return nil
	}

	// always handle window size messages, lest they be lost due to being in the wrong mode
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		d.width, d.height = wsMsg.Width, wsMsg.Height
		d.list.SetSize(d.width, d.height)
		return nil
	}
	keyMsg, isKeyMsg := msg.(tea.KeyMsg)
	var cmd tea.Cmd
	// branch on current mode
	switch d.mode {
	case selecting:
		if isKeyMsg && keyMsg.Type == tea.KeyEnter { // special handling for Enter key
			baseitm := d.list.Items()[d.list.Index()]
			if itm, ok := baseitm.(Item[I]); !ok {
				clilog.Writer.Warnf("failed to type assert %#v as an item", baseitm)
				return tea.Printf(errorNoDeleteText+"\n", "failed type assertion")
			} else {
				d.selectedItem = itm
			}

			// attempt to delete the item
			if err := d.df(d.dryrun, d.selectedItem.id); err != nil {
				d.mode = quitting
				return tea.Printf(errorNoDeleteText+"\n", err)
			}
			if d.dryrun {
				d.mode = quitting
				return tea.Printf(dryrunSuccessText, d.itemSingular, d.selectedItem.id)
			}

			// shift into confirmation mode
			d.mode = selecting
			return textinput.Blink
		}

		d.list, cmd = d.list.Update(msg)
	case confirming:
		if isKeyMsg && keyMsg.Type == tea.KeyEnter {
			// check for confirmation (after cleaning up the input)
			if strings.TrimSpace(strings.ToLower(d.confTI.Value())) == confirmPhrase {
				go d.list.RemoveItem(d.list.Index())
				return tea.Printf(deleteSuccessText,
					d.itemSingular, d.selectedItem.id)
			}
			// any other input, go back to selecting
			d.mode = selecting
			d.confTI.Reset()
			return nil
		}

		d.confTI, cmd = d.confTI.Update(msg)
	}

	return cmd

}

func (d *deleteModel[I]) View() string {
	switch d.mode {
	case quitting:
		// This is unlikely to ever be shown before Mother reasserts control and wipes it
		itm := d.list.SelectedItem()
		if itm == nil {
			return "Not deleting any " + d.itemPlural + "..."
		}
		if searchitm, ok := itm.(Item[I]); !ok {
			clilog.Writer.Warnf("Failed to type assert selected %v", itm)
			return "An error has occurred. Exitting..."
		} else {
			return fmt.Sprintf("Deleting %v...\n", searchitm.Description())
		}
	case selecting:
		return "\n" + d.list.View()
	case confirming:
		var sb strings.Builder

		// display the full item that will be deleted
		sb.WriteString(fmt.Sprintf("Deleting %s (ID: %v):\n"+
			"%v\n"+
			"%v\n",
			d.itemSingular, d.selectedItem.id,
			d.selectedItem.title,
			d.selectedItem.description))
		// request confirmation
		confirmTitle := "Type '" + confirmPhrase + "' to confirm deletion: "
		sb.WriteString(confirmTitle)
		tiView := d.confTI.View()

		// if the line would be too long, bump the ti to a newline
		if lipgloss.Width(confirmTitle)+lipgloss.Width(tiView) > d.width+1 { // 1 cell pad
			sb.WriteString("\n")
		}
		sb.WriteString(tiView)

		return sb.String()
	default:
		clilog.Writer.Warnf("Unknown mode %v", d.mode)
		return "An error has occurred. Exitting..."
	}
}

func (d *deleteModel[I]) Done() bool {
	return d.mode == quitting
}

func (d *deleteModel[I]) Reset() error {
	d.mode = selecting
	d.flagset = flags()
	// the current state of the list is retained
	return nil
}

func (d *deleteModel[I]) SetArgs(_ *pflag.FlagSet, tokens []string) (invalid string, onStart tea.Cmd, err error) {
	var zero I
	// initialize the list
	itms, err := d.ff()
	if err != nil {
		return "", nil, err
	}

	// if there are no items to delete, die
	if len(itms) < 1 {
		d.mode = quitting
		return "", tea.Printf("You have no %v that can be deleted", d.itemPlural), nil
	}

	// while Item[I] satisfies the list.Item interface, Go will not implicitly
	// convert []Item[I] -> []list.Item
	// remember to assert these items as Item[I] on use
	// TODO do we hide this in here, at the cost of an extra n? Or move it out to ff?
	simpleitems := make([]list.Item, len(itms))
	for i := range itms {
		simpleitems[i] = itms[i]
	}

	// create list from the generated delegate
	d.list = listsupport.NewList(simpleitems, 80, 40, d.itemSingular, d.itemPlural)

	// flags and flagset
	if err := d.flagset.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}
	id, dryrun, err := fetchFlagValues[I](&d.flagset)
	if err != nil {
		return "", nil, err
	} else if id != zero { // if id was set, attempt to skip directly to deletion
		d.mode = quitting
		if err := d.df(dryrun, id); err != nil {
			// check for sentinel errors
			// NOTE: this relies on the client log consistently returning 404s as ClientErrors,
			// which I cannot guarentee
			if err, ok := err.(*client.ClientError); ok && err.StatusCode == 404 {
				return "", tea.Printf("Did not find a valid %v with ID %v", d.itemSingular, id), nil
			}
			return "", nil, err
		} else if dryrun {
			return "",
				tea.Printf(dryrunSuccessText, d.itemSingular, id),
				nil
		}
		return "",
			tea.Printf(deleteSuccessText, d.itemSingular, id),
			nil

	}
	d.dryrun = dryrun
	return "", nil, nil
}
