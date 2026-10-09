package scaffold_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldBasicErrorHandling(t *testing.T) {
	newBA := func() action.Pair {
		return scaffold.NewBasicAction("test", "test", "test",
			func(fs *pflag.FlagSet) (success string, addtlCmds tea.Cmd, err error) {
				fail, err := fs.GetBool("fail")
				require.NoError(t, err)
				if fail {
					return "", nil, errors.New("failed")
				}
				return "succeeded", nil, nil
			},
			scaffold.BasicOptions{AddtlFlags: func() *pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("fail", false, "")
				return &fs
			}},
		)
	}

	t.Run("non-interactive", func(t *testing.T) {
		t.Run("fail", func(t *testing.T) {
			ba := newBA()
			ba.Action.SetArgs([]string{"--fail"})
			assert.ErrorContains(t, ba.Action.Execute(), "fail")
		})
		t.Run("success", func(t *testing.T) {
			var sb strings.Builder
			ba := newBA()
			ba.Action.SetOut(&sb)
			ba.Action.SetArgs([]string{})
			assert.NoError(t, ba.Action.Execute())
			require.Equal(t, "succeeded", strings.TrimSpace(sb.String()))
		})
	})
	t.Run("interactive", func(t *testing.T) {
		// unlike the non-interactive tests,
		// running these back to back on the same model is beneficial
		ba := newBA()
		t.Run("fail", func(t *testing.T) {
			inv, _, err := ba.Model.SetArgs(nil, []string{"--fail"}, 80, 60)
			assert.NoError(t, err)
			assert.Empty(t, inv)
			output := ba.Model.Update(nil) // message is irrelevant
			require.Equal(t, "failed", testsupport.ExtractPrintLineMessageString(t, output, false, 0))
			ba.Model.View() // irrelevant
			assert.True(t, ba.Model.Done())
		})
		ba.Model.Reset()
		t.Run("success", func(t *testing.T) {
			inv, _, err := ba.Model.SetArgs(nil, nil, 80, 60)
			assert.NoError(t, err)
			assert.Empty(t, inv)
			output := ba.Model.Update(nil) // message is irrelevant
			require.Equal(t, "succeeded", testsupport.ExtractPrintLineMessageString(t, output, false, 0))
			ba.Model.View() // irrelevant
			assert.True(t, ba.Model.Done())
		})
	})
}
