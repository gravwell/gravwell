//go:build ci

package stylesheet_test

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
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

// Checks that stylesheet.Path wraps each element in the correct ANSI color markers
func TestPathColoring(t *testing.T) {
	// spawn Cur
	stylesheet.Cur = stylesheet.Classic()

	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.ANSI256)
	stylesheet.Cur.Nav = stylesheet.Cur.Nav.Renderer(r)
	stylesheet.Cur.Action = stylesheet.Cur.Action.Renderer(r)
	t.Run("last element is an action", func(t *testing.T) {
		got := stylesheet.Path(true, "nav1", "nav2", "action")
		require.Equal(t, "\x1b[38;5;141mnav1\x1b[0m \x1b[38;5;141mnav2\x1b[0m \x1b[38;5;158maction\x1b[0m", got)
	})
	t.Run("last element is a nav", func(t *testing.T) {
		got := stylesheet.Path(false, "nav1", "nav2", "nav3")
		require.Equal(t, "\x1b[38;5;141mnav1\x1b[0m \x1b[38;5;141mnav2\x1b[0m \x1b[38;5;141mnav3\x1b[0m", got)
	})
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

func TestSegmentedBorder(t *testing.T) {
	type segment = struct {
		StylizedTitle string
		Contents      string
	}
	border := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder())

	t.Run("no segments", func(t *testing.T) {
		if _, err := stylesheet.SegmentedBorder(border, 10); err == nil {
			t.Error("expected an error when no segments are given")
		}
	})
	t.Run("bad width", func(t *testing.T) {
		if _, err := stylesheet.SegmentedBorder(border, 0, segment{"title", "contents"}); err == nil {
			t.Error("expected an error when width is 0")
		}
	})

	tests := []struct {
		name     string
		width    int
		segments []segment
		want     string
	}{
		{"title narrower than box (even remainder)", 10, []segment{{"abcd", "contents"}},
			"┌───abcd───┐\n" +
				"│contents  │\n" +
				"└──────────┘\n"},
		{"title narrower than box (odd remainder)", 10, []segment{{"abc", "contents"}},
			"┌───abc────┐\n" +
				"│contents  │\n" +
				"└──────────┘\n"},
		{"title exactly as wide as box", 4, []segment{{"abcd", "body"}},
			"┌abcd┐\n" +
				"│body│\n" +
				"└────┘\n"},
		{"title one cell wider than box", 4, []segment{{"abcde", "body"}},
			"┌abc…┐\n" +
				"│body│\n" +
				"└────┘\n"},
		{"title much wider than box", 4, []segment{{"a long title", "body"}},
			"┌a l…┐\n" +
				"│body│\n" +
				"└────┘\n"},
		{"later title wider than box", 6, []segment{{"ab", "one"}, {"a long title", "two"}, {"cd", "three"}},
			"┌──ab──┐\n" +
				"│one   │\n" +
				"├a lon…┤\n" +
				"│two   │\n" +
				"├──cd──┤\n" +
				"│three │\n" +
				"└──────┘\n"},
		{"width of 1", 1, []segment{{"title", "b"}},
			"┌…┐\n" +
				"│b│\n" +
				"└─┘\n"},
		{"double-width runes are not split", 6, []segment{{"日本語のタイトル", "body"}},
			"┌日本…─┐\n" +
				"│body  │\n" +
				"└──────┘\n"},
		{"contents wider than box are wrapped", 4, []segment{{"ab", "abcdefgh"}},
			"┌─ab─┐\n" +
				"│abcd│\n" +
				"│efgh│\n" +
				"└────┘\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stylesheet.SegmentedBorder(border, tt.width, tt.segments...)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Error("incorrect border.", testsupport.ExpectedActual("\n"+tt.want, "\n"+got))
			}
			// no line may exceed the requested width (plus the left and right borders)
			for line := range strings.Lines(got) {
				if w := lipgloss.Width(strings.TrimSuffix(line, "\n")); w > tt.width+2 {
					t.Errorf("line %q is %d cells wide; box should be at most %d", line, w, tt.width+2)
				}
			}
		})
	}
}

// Truncating a styled title must not leak its styling into the rest of the border.
func TestSegmentedBorder_StyledTitle(t *testing.T) {
	// force color so the title actually carries ANSI sequences
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI256)
	title := r.NewStyle().Bold(true).Render("a long bold title")

	got, err := stylesheet.SegmentedBorder(lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()), 6,
		struct {
			StylizedTitle string
			Contents      string
		}{title, "body"})
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(got, "\n")
	if w := lipgloss.Width(head); w != 8 {
		t.Error("incorrect head width.", testsupport.ExpectedActual(8, w))
	}
	if want := "┌a lon…┐"; ansi.Strip(head) != want {
		t.Error("incorrect head text.", testsupport.ExpectedActual(want, ansi.Strip(head)))
	}
	// the style must be reset before the closing corner
	if !strings.HasSuffix(head, "\x1b[0m┐") {
		t.Errorf("title styling was not reset before the border resumed: %q", head)
	}
}
