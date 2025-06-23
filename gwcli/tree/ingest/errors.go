package ingest

import (
	"errors"
	"fmt"
)

var illegalTagCharacters = []rune{' '}

var (
	// file path cannot be empty
	errEmptyPath error = errors.New("file path cannot be empty")
	// refusing to ingest an empty file
	errEmptyFile error = errors.New("cowardly refusing to ingest an empty file")
	// a tag contained 1+ of the characters contained in illegalTagCharacters
	errInvalidTagCharacter error = fmt.Errorf("tags cannot contain any of the following characters: %v", illegalTagCharacters)
	// failed to associate a tag to this file using any of the 3 methods (in-line, embedded, default)
	errNoTagSpecified error = errors.New(
		"every file must have a tag in at least one of the following positions (ordered by priority): " +
			"as part of the argument (\"path,tag\"), embedded in the file (in the case of Gravwell JSON files), or via the --default-tag flag")
)

// returned by autoingest if no file paths were given.
// If script is specified, " in script mode" will be appended.
func errNoFilesSpecified(script bool) error {
	tail := ""
	if script {
		tail = " in script mode"
	}
	return fmt.Errorf("at least 1 path must be specified%v", tail)
}
