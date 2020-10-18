// +build windows

package config

import (
	"errors"
	"fmt"
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
