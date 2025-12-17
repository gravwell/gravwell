package cfgdir

import (
	"testing"
)

// TestCfgDir checks that init properly builds paths.
func TestCfgDir(t *testing.T) {
	// just check that none of the paths are empty

	if DefaultRestLogPath == "" {
		t.Errorf("REST log path is not populated")
	}

	if DefaultStdLogPath == "" {
		t.Errorf("dev log path is not populated")
	}

	if DefaultTokenPath == "" {
		t.Errorf("token path is not populated")
	}

}
