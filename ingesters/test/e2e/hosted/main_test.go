package hosted

import (
	"testing"

	"gravwell/e2e"
)

func TestMain(m *testing.M) {
	e2e.Start()

	m.Run()

	e2e.Cleanup()
}
