package queryparams

// parameter keys
const (
	IncludeDeleted string = "include_deleted" // bool: include soft-deleted items in result(s)
	Purge          string = "purge"           // bool: hard delete instead of tombstoning
	Bytes          string = "bytes"           // uint64: # of bytes to preview
)
