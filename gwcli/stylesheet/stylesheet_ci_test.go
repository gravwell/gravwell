package stylesheet_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
func TestViewSubmitButton(t *testing.T) {
	type args struct {
		selected  bool
		err1      string
		err2      string
		paneWidth int
	}
	tests := []struct {
		name string
		args args
		want string // NOTE: actual is space-trimmed
	}{
		{"submit - near min width",
			args{false, "", "", 10},
			`╭──────╮ 
 │submit│ 
 ╰──────╯`},
		{"submit - near min width - with pip",
			args{true, "", "", 10},
			`╭──────╮ 
` + stylesheet.Cur.Pip() + `│submit│ 
 ╰──────╯`},
		{"submit - 60 width - with pip",
			args{true, "", "", 60}, // TODO replace manual repeat calculations
			"╭──────╮" + strings.Repeat(" ", (60/2)-(lipgloss.Width("╭──────╮")/2)) + `
                         >│submit│` + strings.Repeat(" ", 26) + `
                          ╰──────╯`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := strings.TrimSpace(stylesheet.ViewSubmitButton(tt.args.selected, tt.args.err1, tt.args.err2, tt.args.paneWidth))
			if actual != tt.want {
				tt.want = testsupport.Uncloak(tt.want)
				actual = testsupport.Uncloak(actual)
				t.Fatal(testsupport.ExpectedActual(tt.want, actual))
			}
		})
	}
}
