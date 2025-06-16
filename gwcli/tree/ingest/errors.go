package ingest

import "errors"

var (
	errEmptyTag  error = errors.New("tag cannot be empty")
	errEmptyFile error = errors.New("you must select a valid file for ingestion")
)
