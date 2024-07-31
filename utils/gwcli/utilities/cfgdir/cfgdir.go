// cfgdir determines and holds paths for files in the config directory
package cfgdir

import (
	"os"
	"path"
)

// files within the config directory
const (
	tokenName   string = "token"
	restLogName string = "rest.log"
	stdLogName  string = "dev.log"
)

// all persistent data is stored in $os.UserConfigDir/gwcli/
// or local to the instantiation, if that fails
var ( // set by init
	cfgDir             string
	DefaultRestLogPath string
	DefaultStdLogPath  string
	DefaultTokenPath   string
)

// on startup, identify and cache the config directory
func init() {
	const cfgSubFolder = "gwcli"
	cd, err := os.UserConfigDir()
	if err != nil {
		cd = "."
	}
	cfgDir = path.Join(cd, cfgSubFolder)

	// ensure directory's existence
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		// check for exists error
		pe := err.(*os.PathError)
		if pe.Err != os.ErrExist {
			panic("failed to ensure config directory '" + cfgDir + "': " + err.Error())
		}
	}

	// set default paths
	DefaultRestLogPath = path.Join(cfgDir, restLogName)
	DefaultStdLogPath = path.Join(cfgDir, stdLogName)
	DefaultTokenPath = path.Join(cfgDir, tokenName)
}
