//go:build ci

package stylesheet_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

func TestCheckBox(t *testing.T) {
	if tmp := stylesheet.Checkbox(true); tmp != "[✓]" {
		t.Fatal("incorrect checkbox.", testsupport.ExpectedActual("[✓]", tmp))
	}
}
func TestRadiobox(t *testing.T) {
	if tmp := stylesheet.Radiobox(true); tmp != "(✓)" {
		t.Fatal("incorrect checkbox.", testsupport.ExpectedActual("(✓)", tmp))
	}
}

// NOTE(rlandau): these tests are weird, given we are trying to test multiline text "visually".
// The purpose is to watch for breakages inherent to ViewSubmitButton, rather than a caller's screw up.
//
// NOTE2(rlandau): this test assumes the base borders.
// If they change, this test will need to be updated.
func TestViewSubmitButton(t *testing.T) {
	clilog.InitializeFromArgs(nil)

	type args struct {
		selected  bool
		paneWidth int
		errors    []string
	}
	tests := []struct {
		name string
		args args
		want string // NOTE: a newline is prefixed for easier visual checks
	}{
		{"submit - below min width",
			args{false, 6, []string{}},
			`
 ╭──────╮
 │submit│
 ╰──────╯`},
		{"submit - below min width - with pip",
			args{true, 6, nil},
			`
 ╭──────╮
` + stylesheet.Cur.Pip() + `│submit│
 ╰──────╯`},
		{"submit - 60 width - with pip",
			args{true, 60, nil},
			"\n" + strings.Repeat(" ", (60/2)-(lipgloss.Width("╭──────╮")/2)) + "╭──────╮" + strings.Repeat(" ", (60/2)-(lipgloss.Width("╭──────╮")/2)) + `
                         >│submit│` + strings.Repeat(" ", 26) + `
                          ╰──────╯` + strings.Repeat(" ", 26)},
		{"error width < len(err)",
			args{true, 20, []string{"an error longer than the width"}},
			`
   ┌────────────┐   
   │  an error  │   
  >│longer than │   
   │ the width  │   
   └────────────┘   `,
		},
		{"error width == len(err)",
			args{true, 37, []string{"an error equal in length to the width"}},
			`
       ┌──────────────────────┐      
       │  an error equal in   │      
      >│ length to the width  │      
       └──────────────────────┘      `,
		},
		{"error width > len(err)",
			args{true, 42, []string{"an error a little shorter than the width"}},
			`
        ┌─────────────────────────┐       
        │an error a little shorter│       
       >│     than the width      │       
        └─────────────────────────┘       `,
		},
		{"multiple errors; chosen error width < min",
			args{true, 8, []string{"", "second error", ""}},
			`
 ┌─────┐ 
 │secon│ 
>│  d  │ 
 │error│ 
 └─────┘ `,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := "\n" + stylesheet.ViewSubmitButton(tt.args.selected, tt.args.paneWidth, tt.args.errors...)

			if actual != tt.want {
				tt.want = testsupport.Uncloak(tt.want)
				actual = testsupport.Uncloak(actual)
				t.Fatal(testsupport.ExpectedActual(tt.want, actual))
			}
		})
	}
}
