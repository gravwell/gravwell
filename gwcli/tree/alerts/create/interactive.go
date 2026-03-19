package alertscreate

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
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
	stageDispatchers struct {
		m list.Model
	}
	consumersModel list.Model
	metadata       metadata
}

func newCreateModel() *createModel {
	return &createModel{
		// availDispatcher/availConsumer are allocated and set in SetArgs()

		// stages are built in SetArgs as they need to gather data
	}
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch c.stage {
	case stageDispatchers:
		c.stageDispatchers.m, cmd = c.stageDispatchers.m.Update(msg)
	case stageConsumers:
		c.consumersModel, cmd = c.consumersModel.Update(msg)
	case stageMetadata:
		cmd = c.metadata.Update()
	case quitting:
		return nil
	}

	return cmd
}

func (c *createModel) View() string {
	switch c.stage {
	case stageDispatchers:
		return c.stageDispatchers.m.View()
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
	// list models will be reset in SetArgs
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

	// push dispatchers and consumers into their respective lists by wrapping each entry as an item
	dispatchers := make([]list.Item, len(availDispatchers))
	var i int
	for _, dsp := range availDispatchers {
		dispatchers[i] = item{
			Name: dsp.Name,
			Desc: dsp.Description,
			GUID: dsp.GUID,
		}
		i += 1
	}
	c.stageDispatchers = struct{ m list.Model }{
		m: list.New(dispatchers, list.NewDefaultDelegate(), width, height),
	}
	i = 0
	consumers := make([]list.Item, len(availConsumers))
	for _, dsp := range availConsumers {
		consumers[i] = item{
			Name: dsp.Name,
			Desc: dsp.Description,
			GUID: dsp.GUID,
		}
		i += 1
	}
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
