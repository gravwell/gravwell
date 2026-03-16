package flows

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("flows",
		"manage no-code automations",
		"Flows provide a no-code method for developing advanced automations in Gravwell."+
			" Flows are automations, meaning they are normally executed on a user-specified schedule by the search agent.",
		[]string{"flow"},
		nil,
		[]action.Pair{
			list(),
			importCreate(),
			download(),
		},
	)
}

//#region list

// flow is a wrapper struct for list to limit the number of fields displayed.
// Flows return ScheduledSearch by default, which contains too much and some of the data are hard for weave to process.
type flow struct {
	Synced      bool
	ID          int32
	GUID        uuid.UUID
	Groups      []int32
	Global      bool
	WriteAccess types.Access
	Name        string // the name of this scheduled search
	Description string // freeform description
	Labels      []string
	Owner       int32  // uid of owner
	Schedule    string // when to run: a cron spec
	Timezone    string // a location to use for the timezone, e.g. "America/New_York"
	Updated     time.Time
	Disabled    bool

	// if true, search agent will attempt to "backfill" missed runs since
	// the more recent of Updated or LastRun.
	BackfillEnabled bool

	// This sets what kind of scheduled "thing" it is: search, script, or flow
	ScheduledType string

	// Fields for scheduled searches
	SearchReference    uuid.UUID // A reference to a saved query item by UUID. If SearchString is populated on a GET, it represents the query referenced by SearchReference.
	SearchString       string    // The actual search to run. If SearchReference is populated on a GET, SearchString represents the query referenced by SearchReference.
	Duration           int64     // How many seconds back to search, MUST BE NEGATIVE
	SearchSinceLastRun bool      // If set, ignore Duration and run from last run time to now.
	TimeframeOffset    int64     // How many seconds to offset the search timeframe, MUST BE NEGATIVE.

	// For scheduled scripts
	Script         string           // If set, execute the contents rather than running SearchString
	ScriptLanguage types.ScriptLang // what script type is this: anko, go

	// For scheduled flows
	FlowNodeResults map[int]types.FlowNodeResult // results for each node in the flow

	// These fields are updated by the search agent after it runs a search
	//PersistentMaps  map[string]map[string]interface{}
	LastRun         time.Time
	LastRunDuration time.Duration          // how many nanoseconds did it take
	LastSearchIDs   []string               // the IDs of the most recently performed searches
	LastError       string                 // any error from the last run of the scheduled search
	ErrorHistory    []types.ScheduledError // a list of previously-occurring errors
	DebugOutput     []byte                 // output of the script if debugmode was enabled
}

func list() action.Pair {
	return scaffoldlist.NewListAction("list flows", "Lists information about flows you can access.",
		flow{},
		func(fs *pflag.FlagSet) ([]flow, error) {
			baseList, err := connection.Client.GetFlowList()
			if err != nil {
				return nil, err
			}
			flows := make([]flow, len(baseList))
			for i, b := range baseList {
				flows[i] = flow{
					Synced:      b.Synced,
					ID:          b.ID,
					GUID:        b.GUID,
					Groups:      b.Groups,
					Global:      b.Global,
					WriteAccess: b.WriteAccess,
					Name:        b.Name,
					Description: b.Description,
					Labels:      b.Labels,
					Owner:       b.Owner,
					Schedule:    b.Schedule,
					Timezone:    b.Timezone,
					Updated:     b.Updated,
					Disabled:    b.Disabled,

					// if true, search agent will attempt to "backfill" missed runs since
					// the more recent of Updated or LastRun.
					BackfillEnabled: b.BackfillEnabled,

					// This sets what kind of scheduled "thing" it is: search, script, or flow
					ScheduledType: b.ScheduledType,

					// Fields for scheduled searches
					SearchReference:    b.SearchReference,
					SearchString:       b.SearchString,
					Duration:           b.Duration,
					SearchSinceLastRun: b.SearchSinceLastRun,
					TimeframeOffset:    b.TimeframeOffset,

					// For scheduled scripts
					Script:         b.Script,
					ScriptLanguage: b.ScriptLanguage,

					// For scheduled flows
					//FlowJSON: b.Flow, // Disabled. Users should call download instead.
					/*FlowNodeResults map[int]types.FlowNodeResult // results for each node in the flow

					// These fields are updated by the search agent after it runs a search
					//PersistentMaps  map[string]map[string]interface{} */
					LastRun:         b.LastRun,
					LastRunDuration: b.LastRunDuration,
					LastSearchIDs:   b.LastSearchIDs,
					LastError:       b.LastError,
					ErrorHistory:    b.ErrorHistory,
				}
			}
			return flows, nil
		},
		scaffoldlist.Options{},
	)
}

//#endregion list

// importCreate is the create function for flows, but the flow itself is created from JSON slurped from a file
func importCreate() action.Pair {
	return scaffoldcreate.NewCreateAction("flow",
		scaffoldcreate.Config{
			"name":      scaffoldcreate.FieldName("flow"),
			"desc":      scaffoldcreate.FieldDescription("flow"),
			"frequency": scaffoldcreate.FieldFrequency(),
			"path":      scaffoldcreate.FieldPath("file containing a flow in JSON form"),
			"groups": scaffoldcreate.Field{
				Required:      false,
				Title:         "Groups",
				Usage:         "comma-separated list of group IDs this flow is accessible to",
				Type:          scaffoldcreate.Text,
				FlagName:      "groups",
				FlagShorthand: 'g',
				Order:         40,
			},
		},
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// slurp the json file
			var json string
			if b, err := os.ReadFile(fieldValues["path"]); err != nil {
				return 0, err.Error(), nil // this is probably a file permission or exist error so return as invalid
			} else {
				json = strings.TrimSpace(string(b))
			}

			// coerce groups
			var groups []int32
			for _, s := range strings.Split(fieldValues["groups"], ",") {
				group, err := strconv.ParseInt(s, 10, 32)
				if err != nil {
					clilog.Writer.Warnf("failed to parse %v as int32 for groupID: %v", s, err)
					continue
				}
				groups = append(groups, int32(group))
			}

			id, err = connection.Client.CreateFlow(fieldValues["name"], fieldValues["desc"], fieldValues["frequency"], json, groups)
			return id, "", err
		},
		scaffoldcreate.Options{Use: "import"})
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download the JSOn representation of a flow",
		"Download a flow as JSON so it can be re-imported later. Flows can be specified by ID or GUID.\n"+
			"Prints to STDOUT unless -o is specified.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			flow, err := connection.Client.GetFlow(fs.Arg(0))
			if err != nil {
				return err.Error(), nil
			}
			// check for output
			if outPath, err := fs.GetString(ft.Output.Name()); err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			} else if outPath != "" {
				out, err := os.Create(outPath)
				if err != nil {
					clilog.Writer.Warnf("failed to open %v for writing: %v", outPath, err)
					return
				}
				defer out.Close()
				n, err := out.WriteString(flow.Flow)
				if err != nil {
					return err.Error(), nil
				}
				return stylesheet.StringWriteToFileSuccess(n, outPath), nil
			}
			// spit to terminal
			return flow.Flow, nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				ft.Output.Register(&fs)
				return fs
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return "you must specify exactly 1 argument (flow ID or flow GUID)", nil
				}
				return "", nil
			}})
}
