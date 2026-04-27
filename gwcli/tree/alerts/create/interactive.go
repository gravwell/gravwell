package alertscreate

import (
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/spf13/pflag"
)

type stage = uint

const (
	stageDispatchers stage = iota
	stageConsumers
	stageMetadata
	quitting // work is done, just waiting for mother to reassert control
)

// the model operates in stages.
// 1) pick dispatchers
// 2) pick consumers
// 3) set metadata
type createModel struct {
	// stages
	stage            stage
	dispatchersModel multiselectlist.Model[string]
	consumersModel   multiselectlist.Model[string]
	metadata         *metadata
}

func newCreateModel() *createModel {
	return &createModel{
		metadata: NewMetadata(),
		// list models are generated in SetArgs
	}
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	var retCmd tea.Cmd
	switch c.stage {
	case stageDispatchers:
		c.dispatchersModel, retCmd = c.dispatchersModel.Update(msg)
		if c.dispatchersModel.Done() {
			c.stage = stageConsumers
		}
	case stageConsumers:
		c.consumersModel, retCmd = c.consumersModel.Update(msg)
		if c.consumersModel.Done() {
			c.stage = stageMetadata
		}
	case stageMetadata:
		if cmd, backToDispatchers, backToConsumers, trySubmit := c.metadata.Update(msg); trySubmit {
			// coalesce all of our data into an alert definition
			var maxEvents int
			if str := c.metadata.maxEvents.Value(); str != "" {
				me, err := strconv.ParseInt(str, 10, 32)
				if err != nil {
					c.metadata.submitErr = err.Error()
					return nil
				}
				maxEvents = int(me)
			}
			var retainSeconds int
			if str := c.metadata.retain.Value(); str != "" {
				r, err := time.ParseDuration(str)
				if err != nil {
					c.metadata.submitErr = err.Error()
					return nil
				}
				retainSeconds = int(r.Seconds())
			}

			dispatchers := []types.AlertDispatcher{}
			for _, li := range c.dispatchersModel.GetSelectedItems() {
				dispatchers = append(dispatchers, types.AlertDispatcher{ID: li.ID(), Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH})
			}
			consumers := []types.AlertConsumer{}
			for _, li := range c.consumersModel.GetSelectedItems() {
				consumers = append(consumers, types.AlertConsumer{ID: li.ID(), Type: types.ALERTCONSUMERTYPE_FLOW})
			}
			ad := types.Alert{
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

			// try to submit
			res, err := connection.Client.CreateAlert(ad)
			if err != nil {
				c.metadata.submitErr = err.Error()
				return nil
			}
			c.stage = quitting
			return tea.Println(phrases.SuccessfullyCreatedItem("alert", res.ID))
		} else if backToDispatchers {
			c.stage = stageDispatchers
			c.dispatchersModel.Undone()
			c.consumersModel.Undone()
		} else if backToConsumers {
			c.stage = stageConsumers
			c.consumersModel.Undone()
		} else {
			retCmd = cmd
		}
	case quitting:
		return nil
	}

	return retCmd
}

func (c *createModel) View() string {
	switch c.stage {
	case stageDispatchers:
		return c.dispatchersModel.View()
	case stageConsumers:
		return c.consumersModel.View()
	case stageMetadata:
		return c.metadata.View()
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

	// empty out the structs

	// models will be rebuilt on the next SetArgs
	c.dispatchersModel = multiselectlist.Model[string]{}
	c.consumersModel = multiselectlist.Model[string]{}
	c.metadata.Reset()
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
				ID_:          dsp.ID,
			}
			i += 1
		}

		c.dispatchersModel = multiselectlist.New(dispatchers, width, height, multiselectlist.Options{})
		c.dispatchersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.dispatchersModel.StatusMessageOnSelect = true
	})

	consumers := make([]multiselectlist.SelectableItem[string], len(availConsumers))
	wg.Go(func() {
		var i uint
		for _, cns := range availConsumers {
			consumers[i] = &multiselectlist.DefaultSelectableItem[string]{
				Title_:       cns.Name,
				Description_: cns.Description,
				ID_:          cns.ID,
			}
			i += 1
		}
		c.consumersModel = multiselectlist.New(consumers, width, height, multiselectlist.Options{})
		c.consumersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.consumersModel.StatusMessageOnSelect = true
	})
	wg.Wait()

	// prepopulate metadata
	c.metadata.Init(flagVals.name, flagVals.description, flagVals.tag, flagVals.enabled, flagVals.maxEvents, flagVals.retain)
	return "", nil, nil
}
