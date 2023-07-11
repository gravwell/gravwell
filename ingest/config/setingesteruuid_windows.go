//go:build windows
// +build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// SetIngesterUUID modifies the configuration file at loc, setting the
// Ingester-UUID parameter to the given UUID. This function allows ingesters
// to assign themselves a UUID if one is not given in the configuration file.
func (ic *IngestConfig) SetIngesterUUID(id uuid.UUID, loc string) (err error) {
	if zeroUUID(id) {
		return errors.New("UUID is empty")
	}
	var content string
	if content, err = reloadContent(loc); err != nil {
		return
	}
	//crack the config file into lines
	lines := strings.Split(content, "\n")
	lo := argInGlobalLines(lines, uuidParam)
	if lo == -1 {
		//UUID value not set, insert immediately after global
		gStart, _, ok := globalLineBoundary(lines)
		if !ok {
			err = ErrGlobalSectionNotFound
			return
		}
		lines, err = insertLine(lines, fmt.Sprintf(`%s="%s"`, uuidParam, id.String()), gStart+1)
	} else {
		//found it, update it
		lines, err = updateLine(lines, uuidParam, fmt.Sprintf(`"%s"`, id), lo)
	}
	if err != nil {
		return
	}
	ic.Ingester_UUID = id.String()
	content = strings.Join(lines, "\n")
	err = updateConfigFile(loc, content)
	return
}

// THIS RACY, there is no good atomic file operations on windows, so... good luck
func updateConfigFile(loc string, content string) error {
	if loc == `` {
		return errors.New("Configuration was loaded with bytes, cannot update")
	}
	dirname := filepath.Dir(loc)
	filename := filepath.Base(loc)
	if filename == `.` {
		return errors.New("config filepath is a directory or empty")
	}
	fout, err := os.CreateTemp(dirname, filename)
	if err != nil {
		return err
	}
	tname := fout.Name()
	if err := writeFull(fout, []byte(content)); err != nil {
		fout.Close()
		os.Remove(tname)
		return err
	} else if err = fout.Close(); err != nil {
		return err
	}
	return os.Rename(tname, loc)
}
