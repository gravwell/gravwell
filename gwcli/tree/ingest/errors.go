package ingest

import (
	"errors"
	"fmt"
)

var illegalTagCharacters = []rune{' '}

var (
	errEmptyTag            error = errors.New("tag cannot be empty")
	errEmptyFile           error = errors.New("you must select a valid file for ingestion")
	errInvalidTagCharacter error = fmt.Errorf("tags cannot contain any of the following characters: %v", illegalTagCharacters)
)
