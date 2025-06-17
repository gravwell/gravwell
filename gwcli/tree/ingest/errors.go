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
	// returned by autoingest if no file paths were given
	errNoFilesSpecified error = errors.New("at least 1 file path must be specified")
)

func errBadTagCount(count uint) error {
	return fmt.Errorf("tag count must be 1 or equal to the number of files specified (%v)", count)
}
