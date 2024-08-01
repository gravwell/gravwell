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
	ft "gwcli/stylesheet/flagtext"
	"gwcli/utilities/listsupport"
	"gwcli/utilities/scaffold"
	"gwcli/utilities/treeutils"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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
)

type deleteModel[I scaffold.Id_t] struct {
	itemSingular string // "macro", "kit", "query"
	itemPlural   string // "macros", "kits", "queries"
	mode         mode   // current mode
	list         list.Model

	flagset pflag.FlagSet // parsed flag values (set in SetArgs)
	dryrun  bool

	df deleteFunc[I] // function to delete an item
	ff fetchFunc[I]  // function to get all delete-able items
}

func newDeleteModel[I scaffold.Id_t](del deleteFunc[I], fch fetchFunc[I]) *deleteModel[I] {
	d := &deleteModel[I]{mode: selecting}
	d.flagset = flags()

	d.df = del
	d.ff = fch

	return d
}

func (d *deleteModel[I]) Update(msg tea.Msg) tea.Cmd {
	if d.Done() {
		return nil
	}
	if len(d.list.Items()) == 0 {
		d.mode = quitting
		return tea.Printf("You have no %v that can be deleted", d.itemPlural)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.list.SetSize(msg.Width, msg.Height)
		return nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyEnter {
			var (
				baseitm list.Item // item stored in the list
				itm     Item[I]   // baseitm cast to our expanded item type
				ok      bool      // type assertion result
			)
			baseitm = d.list.Items()[d.list.Index()]
			if itm, ok = baseitm.(Item[I]); !ok {
				clilog.Writer.Warnf("failed to type assert %#v as an item", baseitm)
				return tea.Printf(errorNoDeleteText+"\n", "failed type assertion")
			}
			d.mode = quitting

			// attempt to delete the item
			if err := d.df(d.dryrun, itm.id); err != nil {
				return tea.Printf(errorNoDeleteText+"\n", err)
			}
			if d.dryrun {
				return tea.Printf(dryrunSuccessText,
					d.itemSingular, itm.id)
			} else {
				go d.list.RemoveItem(d.list.Index())
				return tea.Printf(deleteSuccessText,
					d.itemSingular, itm.id)
			}
		}
	}

	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)

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
