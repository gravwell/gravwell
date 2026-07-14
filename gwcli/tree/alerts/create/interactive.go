package alertscreate

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/confirmation"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/pflag"
)

const mslHeighBuffer uint = 2 // how many lines to buffer the height fed to each MSL

type stage = uint

const (
	stageDispatchers stage = iota
	stageConsumers
	stageMetadata
	stageConfirm
	quitting // work is done, just waiting for mother to reassert control
)

// the model operates in stages.
// 1) pick dispatchers
// 2) pick consumers
// 3) set metadata
// 4) confirm
type createModel struct {
	stage            stage
	dispatchersModel multiselectlist.Model[string]
	consumersModel   multiselectlist.Model[string]
	metadata         *metadata

	alertToCreate types.Alert

	confirmation confirmation.Model
}

func newCreateModel() *createModel {
	cm := &createModel{
		// MSLs are generated in SetArgs
		metadata: NewMetadata(),
	}
	cm.Reset()
	return cm
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	// intercept WSMs to buffer height for header lines
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		wsm.Height -= 8
		var cmds = make([]tea.Cmd, 2)
		c.dispatchersModel, cmds[0] = c.dispatchersModel.Update(wsm)
		c.consumersModel, cmds[1] = c.consumersModel.Update(wsm)
		return tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	switch c.stage {
	case stageDispatchers:
		c.dispatchersModel, cmd = c.dispatchersModel.Update(msg)
		if c.dispatchersModel.Done() {
			c.stage = stageConsumers
		}
		return cmd
	case stageConsumers:
		c.consumersModel, cmd = c.consumersModel.Update(msg)
		if c.consumersModel.Done() {
			c.stage = stageMetadata
			c.metadata.toggleFocus(false)
			c.metadata.selected = metaName
			c.metadata.toggleFocus(true)
		}
		return cmd
	case stageMetadata:
		cmd, done := c.metadata.Update(msg)
		if done {
			// coalesce all of our data into an alert definition
			var maxEvents int
			if str := c.metadata.maxEvents.Value(); str != "" {
				me, err := strconv.ParseInt(str, 10, 32)
				if err != nil {
					c.metadata.inputErr = err.Error()
					return cmd
				}
				maxEvents = int(me)
			}
			var retainSeconds int
			if str := c.metadata.retain.Value(); str != "" {
				r, err := time.ParseDuration(str)
				if err != nil {
					c.metadata.inputErr = err.Error()
					return cmd
				}
				retainSeconds = int(r.Seconds())
			}

			dispatchers := []types.AlertDispatcher{}
			hlDispatchers := []string{}
			for _, li := range c.dispatchersModel.GetSelectedItems() {
				dispatchers = append(dispatchers, types.AlertDispatcher{ID: li.ID(), Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH})
				hlDispatchers = append(hlDispatchers, "\""+li.Title()+"\"")
			}
			consumers := []types.AlertConsumer{}
			hlConsumers := []string{}
			for _, li := range c.consumersModel.GetSelectedItems() {
				consumers = append(consumers, types.AlertConsumer{ID: li.ID(), Type: types.ALERTCONSUMERTYPE_FLOW})
				hlConsumers = append(hlConsumers, "\""+li.Title()+"\"")
			}
			c.alertToCreate = types.Alert{
				CommonFields: types.CommonFields{
					Name:        c.metadata.name.Value(),
					Description: c.metadata.description.Value(),
				},
				TargetTag:          c.metadata.tag.Value(),
				Disabled:           !c.metadata.enable,
				MaxEvents:          int(maxEvents),
				SaveSearchDuration: int32(retainSeconds),
				SaveSearchEnabled:  retainSeconds != 0,

				Dispatchers: dispatchers,
				Consumers:   consumers,
			}

			c.stage = stageConfirm
			// set header lines
			c.confirmation.HeaderLines = []string{
				"Creating new alert \"" + c.metadata.name.Value() + "\"",
				"with " + strconv.FormatInt(int64(len(hlDispatchers)), 10) + " dispatchers",
				"[" + strings.Join(hlDispatchers, " ") + "]",
				"and " + strconv.FormatInt(int64(len(hlConsumers)), 10) + " consumers",
				"[" + strings.Join(hlConsumers, " ") + "]",
			}
		}
		return cmd
	case stageConfirm:
		var (
			done, submit bool
			choice       uint
		)
		if c.confirmation, cmd, done, submit, choice = c.confirmation.Update(msg); done {
			if submit {
				c.stage = quitting
				res, err := connection.Client.CreateAlert(c.alertToCreate)
				if err != nil {
					return tea.Printf("failed to create alert: %v", err)
				}
				return tea.Println(phrases.SuccessfullyCreatedItem("alert", res.ID))
			}
			// return to a prior stage
			switch choice {
			case stageDispatchers:
				c.stage = stageDispatchers
				c.dispatchersModel.Undone()
				c.consumersModel.Undone()
			case stageConsumers:
				c.stage = stageConsumers
				c.consumersModel.Undone()
			case stageMetadata:
				c.stage = stageMetadata
			}
			return nil
		}
		return cmd
	case quitting:
		return nil
	}
	clilog.Writer.Warn("fell-through stage handling", log.KV("stage", c.stage))
	return nil
}

func (c *createModel) View() string {
	switch c.stage {
	case stageDispatchers:
		return c.dispatchersModel.View()
	case stageConsumers:
		return c.consumersModel.View()
	case stageMetadata:
		return c.metadata.View()
	case stageConfirm:
		return c.confirmation.View()
	default:
		clilog.Writer.Errorf("cannot view unknown stage %v", c.stage)
		return ""
	}
}

func (c *createModel) Done() bool {
	return c.stage == quitting
}

func (c *createModel) Reset() error {
	c.stage = stageDispatchers

	// models will be rebuilt on the next SetArgs
	c.dispatchersModel = multiselectlist.Model[string]{}
	c.consumersModel = multiselectlist.Model[string]{}
	c.metadata.Reset()
	c.confirmation = confirmation.Model{}
	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	availDispatchers, availConsumers, invalid, err := prerequisites()
	if err != nil || invalid != "" {
		return invalid, nil, err
	}
	fs := createFlagSet()
	if err := fs.Parse(tokens); err != nil {
		return "", nil, err
	}
	flagVals, inv := readFlags(fs)
	if inv != "" {
		return inv, nil, nil
	}
	// check if we can complete this request without interactivity
	if inv, alert := validateFlagValues(availConsumers, availDispatchers, flagVals); inv == "" {
		res, err := connection.Client.CreateAlert(alert)
		if err != nil {
			return "", nil, err
		}
		c.stage = quitting
		return "", tea.Println(phrases.SuccessfullyCreatedItem("alert", res.ID)), nil
	}

	// push dispatchers into their respective lists by wrapping each entry as an item
	var wg sync.WaitGroup

	dispatchers := make([]multiselectlist.SelectableItem[string], len(availDispatchers))
	wg.Go(func() {
		var i uint
		for _, dsp := range availDispatchers {
			dispatchers[i] = &multiselectlist.DefaultSelectableItem[string]{
				Title_:       dsp.Name,
				Description_: dsp.Description,
				Selected_:    slices.Contains(flagVals.dispatcherIDs, dsp.ID),
				ID_:          dsp.ID,
			}
			i += 1
		}

		c.dispatchersModel = multiselectlist.New(dispatchers, width, height-int(mslHeighBuffer), multiselectlist.Options{})
		c.dispatchersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.dispatchersModel.StatusMessageOnSelect = true
		c.dispatchersModel.Title = "Select Dispatchers"
	})

	consumers := make([]multiselectlist.SelectableItem[string], len(availConsumers))
	wg.Go(func() {
		var i uint
		for _, cns := range availConsumers {
			consumers[i] = &multiselectlist.DefaultSelectableItem[string]{
				Title_:       cns.Name,
				Description_: cns.Description,
				Selected_:    slices.Contains(flagVals.consumerIDs, cns.ID),
				ID_:          cns.ID,
			}
			i += 1
		}
		c.consumersModel = multiselectlist.New(consumers, width, height-int(mslHeighBuffer), multiselectlist.Options{})
		c.consumersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.consumersModel.StatusMessageOnSelect = true
		c.consumersModel.Title = "Select Consumers"
	})
	wg.Wait()

	// prepopulate metadata
	c.metadata.Init(flagVals.name, flagVals.description, flagVals.tag, flagVals.enabled, flagVals.maxEvents, flagVals.retain)

	c.confirmation.Init([]string{"dispatchers", "consumers", "metadata"}, uint(width), uint(height))
	return "", nil, nil
}
