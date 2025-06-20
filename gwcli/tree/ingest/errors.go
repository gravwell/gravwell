package ingest

import (
	"errors"
	"fmt"
)

var illegalTagCharacters = []rune{' '}

var (
	errEmptyTag  error = errors.New("tag cannot be empty")
	errEmptyFile error = errors.New("you must select a valid file for ingestion")
	// a tag contained 1+ of the characters contained in illegalTagCharacters
	errInvalidTagCharacter error = fmt.Errorf("tags cannot contain any of the following characters: %v", illegalTagCharacters)
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
func errBadTagCount(count uint) error {
	return fmt.Errorf("tag count must be 1 or equal to the number of files specified (%v)", count)
}
