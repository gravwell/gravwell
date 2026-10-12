// Package queryparams manages data we transmit via query parameters.
package queryparams

// parameter keys
const (
	IncludeDeleted string = "include_deleted" // bool: include soft-deleted items in result(s)
	Purge          string = "purge"           // bool: hard delete instead of tombstoning
	Bytes          string = "bytes"           // uint64: # of bytes to preview
	// ExpectedVersion makes a table row delete conditional on the row's
	// current version (int64).
	ExpectedVersion string = "expected_version"
	// All requests items owned by every user, not just the caller.
	// Only effective for admins.
	//
	// List endpoints use QueryOptions.All instead.
	All string = "all"
)
