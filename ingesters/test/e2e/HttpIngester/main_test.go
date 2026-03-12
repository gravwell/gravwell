package HttpIngester

import (
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/test/e2e"
)

func TestMain(m *testing.M) {
	e2e.Start()

	m.Run()

	e2e.Cleanup()
}
