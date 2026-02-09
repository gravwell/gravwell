// Package storage contains implementations of the storage interface for hosted runtimes.
package storage

import "errors"

var (
	ErrStorageNotFound = errors.New("storage not found")
)
