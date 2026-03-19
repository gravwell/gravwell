package alerts

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/spf13/pflag"
)

type createModel struct {
	// meta controls
	done bool

	availDispatchers map[uuid.UUID]types.ScheduledSearch
	availConsumers   map[uuid.UUID]types.ScheduledSearch
}

func newCreateModel() *createModel {
	return &createModel{}
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	return nil
}

func (c *createModel) View() string {
	return ""
}

func (c *createModel) Done() bool {
	return c.done
}

func (c *createModel) Reset() error {
	c.done = false
	c.availDispatchers, c.availConsumers = nil, nil

	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string, width, height int) (invalid string, onStart tea.Cmd, err error) {
	c.availDispatchers, c.availConsumers, invalid, err = prerequisites()
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
	if inv, alert := validateFlagValues(c.availConsumers, c.availDispatchers, flagVals); inv == "" {
		res, err := connection.Client.NewAlert(alert)
		if err != nil {
			return "", nil, err
		}
		c.done = true
		return "", tea.Println(phrases.SuccessfullyCreatedItem("alert", res.ThingUUID.String())), nil
	}

	// TODO interactivity
	c.done = true
	return "", phrases.InteractivityNYI(), nil
}
