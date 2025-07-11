package stylesheet_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

func TestCheckBox(t *testing.T) {
	if tmp := stylesheet.Checkbox(true); tmp != "[✓]" {
		t.Fatal("incorrect checkbox.", testsupport.ExpectedActual("[✓]", tmp))
	}
}
func TestRadopbox(t *testing.T) {
	if tmp := stylesheet.Radiobox(true); tmp != "(✓)" {
		t.Fatal("incorrect checkbox.", testsupport.ExpectedActual("(✓)", tmp))
	}
}
