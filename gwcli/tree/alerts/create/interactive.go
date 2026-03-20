package alertscreate

import (
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
	dispatchersModel list.Model
	consumersModel   list.Model
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
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.Type {
			case tea.KeyEnter:
				c.stage = stageConsumers
				return nil
			case tea.KeySpace:
				li, ok := c.dispatchersModel.SelectedItem().(item)
				if !ok {
					clilog.Writer.Errorf("failed to cast dispatcher from item. Bare item: %v", li)
					return nil
				}
				li.Selected = !li.Selected
				// reinsert the item

				cmd := c.dispatchersModel.SetItem(c.dispatchersModel.GlobalIndex(), li)

				var statusMsg string
				if li.Selected {
					statusMsg = "selected"
				} else {
					statusMsg = "deselected"
				}
				statusMsg += " dispatcher " + li.Title()
				return tea.Batch(cmd, c.dispatchersModel.NewStatusMessage(statusMsg))
			}
		}

		c.dispatchersModel, retCmd = c.dispatchersModel.Update(msg)
	case stageConsumers:
		c.consumersModel, retCmd = c.consumersModel.Update(msg)
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
			for _, li := range c.dispatchersModel.Items() {
				dsp, ok := li.(item)
				if !ok {
					clilog.Writer.Errorf("failed to cast dispatcher from item. Bare item: %v", li)
					continue
				}
				if dsp.Selected {
					dispatchers = append(dispatchers, types.AlertDispatcher{ID: dsp.GUID.String(), Type: types.ALERTDISPATCHERTYPE_SCHEDULEDSEARCH})
				}
			}
			consumers := []types.AlertConsumer{}
			for _, li := range c.consumersModel.Items() {
				cns, ok := li.(item)
				if !ok {
					clilog.Writer.Errorf("failed to cast consumer from item. Bare item: %v", li)
					continue
				}
				if cns.Selected {
					consumers = append(consumers, types.AlertConsumer{ID: cns.GUID.String(), Type: types.ALERTCONSUMERTYPE_FLOW})
				}
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

	// list models will be rebuilt on the next SetArgs
	c.dispatchersModel = list.Model{}
	c.consumersModel = list.Model{}
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

	dispatchers := make([]list.Item, len(availDispatchers))
	wg.Go(func() {
		// if the user pre-selected dispatchers, make sure they are selected in the list
		selected := make(map[uuid.UUID]bool, len(flagVals.dispatcherIDs))
		for _, uid := range flagVals.dispatcherIDs {
			selected[uid] = true
		}
		var i int
		for _, dsp := range availDispatchers {
			dispatchers[i] = item{
				Name:     dsp.Name,
				Desc:     dsp.Description,
				GUID:     dsp.GUID,
				Selected: selected[dsp.GUID],
			}
			i += 1
		}
		c.dispatchersModel = list.New(dispatchers, list.NewDefaultDelegate(), width, height)
		c.dispatchersModel.StatusMessageLifetime = stylesheet.StatusMessageLifetime
	})

	consumers := make([]list.Item, len(availConsumers))
	wg.Go(func() {
		// if the user pre-selected dispatchers, make sure they are selected in the list
		selected := make(map[uuid.UUID]bool, len(flagVals.consumerGUIDs))
		for _, uid := range flagVals.consumerGUIDs {
			selected[uid] = true
		}
		var i int
		for _, cns := range availConsumers {
			consumers[i] = item{
				Name:     cns.Name,
				Desc:     cns.Description,
				GUID:     cns.GUID,
				Selected: selected[cns.GUID],
			}
			i += 1
		}
	})
	wg.Wait()

	// prepopulate data
	c.consumersModel = list.New(consumers, list.NewDefaultDelegate(), width, height)
	c.metadata.Init(flagVals.name, flagVals.description, flagVals.tag, flagVals.enabled, flagVals.maxEvents, flagVals.retain)
	return "", nil, nil
}

type item struct {
	Name     string
	Desc     string
	GUID     uuid.UUID
	Selected bool // is this item currently selected?
}

// FilterValue sets the string to include/disclude this item on when a user filters.
func (i item) FilterValue() string {
	return i.Name
}

func (i item) Title() string {
	return stylesheet.Checkbox(i.Selected) + " " + i.Name
}

func (i item) Description() string {
	return i.Desc
}
