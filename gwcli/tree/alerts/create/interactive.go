package alertscreate

import (
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
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
	dispatchersModel multiselectlist.Model
	consumersModel   multiselectlist.Model
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
		if cmd, trySubmit, backToDispatchers, backToConsumers := c.metadata.Update(msg); trySubmit {
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
				dsp, ok := li.(item)
				if !ok {
					clilog.Writer.Errorf("failed to cast dispatcher from item. Bare item: %v", li)
					continue
				}
				dispatchers = append(dispatchers, types.AlertDispatcher{ID: dsp.GUID.String(), Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH})
			}
			consumers := []types.AlertConsumer{}
			for _, li := range c.consumersModel.Items() {
				cns, ok := li.(item)
				if !ok {
					clilog.Writer.Errorf("failed to cast consumer from item. Bare item: %v", li)
					continue
				}

				consumers = append(consumers, types.AlertConsumer{ID: cns.GUID.String(), Type: types.ALERTCONSUMERTYPE_FLOW})
			}
			ad := types.AlertDefinition{
				Name:               c.metadata.name.Value(),
				Description:        c.metadata.description.Value(),
				TargetTag:          c.metadata.tag.Value(),
				Disabled:           !c.metadata.enable,
				MaxEvents:          int(maxEvents),
				SaveSearchDuration: int32(retainSeconds),
				SaveSearchEnabled:  retainSeconds != 0,

				Dispatchers: dispatchers,
				Consumers:   consumers,
			}

			// try to submit
			res, err := connection.Client.NewAlert(ad)
			if err != nil {
				c.metadata.submitErr = err.Error()
				return nil
			}
			c.stage = quitting
			return tea.Println(phrases.SuccessfullyCreatedItem("alert", res.GUID.String()))
		} else if backToDispatchers {
			c.stage = stageDispatchers
		} else if backToConsumers {
			c.stage = stageConsumers
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
	c.dispatchersModel = multiselectlist.Model{}
	c.consumersModel = multiselectlist.Model{}
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
		res, err := connection.Client.NewAlert(alert)
		if err != nil {
			return "", nil, err
		}
		c.stage = quitting
		return "", tea.Println(phrases.SuccessfullyCreatedItem("alert", res.ThingUUID.String())), nil
	}

	// push dispatchers into their respective lists by wrapping each entry as an item
	var wg sync.WaitGroup

	dispatchers := make([]list.DefaultItem, len(availDispatchers))
	wg.Go(func() {
		preselected := make(map[uint]bool, len(flagVals.dispatcherIDs))
		var i uint
		for _, dsp := range availDispatchers {
			dispatchers[i] = item{
				Name: dsp.Name,
				Desc: dsp.Description,
				GUID: dsp.GUID,
			}
			// this sucks from a time-complexity standpoint but ¯\_(ツ)_/¯
			if slices.Contains(flagVals.dispatcherIDs, dsp.GUID) {
				preselected[i] = true
			}
			i += 1
		}

		c.dispatchersModel = multiselectlist.New(dispatchers, width, height, multiselectlist.Options{
			Preselected: preselected,
		})
		c.dispatchersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.dispatchersModel.StatusMessageOnSelect = true
	})

	consumers := make([]list.DefaultItem, len(availConsumers))
	wg.Go(func() {
		preselected := make(map[uint]bool, len(flagVals.consumerGUIDs))
		var i uint
		for _, cns := range availConsumers {
			consumers[i] = item{
				Name: cns.Name,
				Desc: cns.Description,
				GUID: cns.GUID,
			}
			// this sucks from a time-complexity standpoint but ¯\_(ツ)_/¯
			if slices.Contains(flagVals.consumerGUIDs, cns.GUID) {
				preselected[i] = true
			}
			i += 1
		}
		c.consumersModel = multiselectlist.New(consumers, width, height, multiselectlist.Options{
			Preselected: preselected,
		})
		c.dispatchersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
		c.dispatchersModel.StatusMessageOnSelect = true
	})
	wg.Wait()

	// prepopulate metadata
	c.metadata.Init(flagVals.name, flagVals.description, flagVals.tag, flagVals.enabled, flagVals.maxEvents, flagVals.retain)
	return "", nil, nil
}

type item struct {
	Name string
	Desc string
	GUID uuid.UUID
}

// FilterValue sets the string to include/disclude this item on when a user filters.
func (i item) FilterValue() string {
	return i.Name
}

func (i item) Title() string {
	return i.Name
}

func (i item) Description() string {
	return i.Desc
}
