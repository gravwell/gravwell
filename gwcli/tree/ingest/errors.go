package ingest

import (
	"errors"
	"fmt"
	"strings"
)

var illegalTagCharacters = []rune{'!', '@', '#', '$', '%',
	'^', '&', '*', '(', ')',
	'=', '+', '<', '>', ',',
	'.', ':', ';', '{', '}',
	'[', ']', '|', '\\'}

var (
	// file path cannot be empty
	errEmptyPath error = errors.New("file path cannot be empty")
	// refusing to ingest an empty file
	errEmptyFile error = errors.New("cowardly refusing to ingest an empty file")
	// a tag contained 1+ of the characters contained in illegalTagCharacters
	// tags cannot contain illegal characters.
	// set by init
	errInvalidTagCharacter error
	// failed to associate a tag to this file using any of the 3 methods (in-line, embedded, default)
	errNoTagSpecified error = errors.New(
		"every file must have a tag in at least one of the following positions (ordered by priority): " +
			"as part of the argument (\"path,tag\"), embedded in the file (in the case of Gravwell JSON files), or via the --default-tag flag")
)

func init() {
	var sb strings.Builder
	for _, r := range illegalTagCharacters {
		sb.WriteString("'" + string(r) + "'" + ",")
	}

	errInvalidTagCharacter = fmt.Errorf("tags cannot contain any of the following characters:\n%v", sb.String()[:sb.Len()-1]) // chip the last comma
}

// returned by autoingest if no file paths were given.
// If script is specified, " in script mode" will be appended.
func errNoFilesSpecified(script bool) error {
	tail := ""
	if script {
		tail = " in script mode"
	}
	return fmt.Errorf("at least 1 path must be specified%v", tail)
}

// thrown by ingest when it received a directory after supposedly collecting all files were collected
func errUnwalkedDirectory(pth string) error {
	return fmt.Errorf("'%v' is a directory, which should have been traversed prior. Please file a bug report.", pth)
}
