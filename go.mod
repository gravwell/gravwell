module github.com/gravwell/gravwell/v3

go 1.13

require (
	github.com/bxcodec/faker/v3 v3.3.1
	github.com/gravwell/ingest/v3 v3.3.12
	go.etcd.io/bbolt v1.3.4
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1
)

// Leave this until https://github.com/buger/jsonparser/pull/180 is merged
replace github.com/buger/jsonparser => github.com/floren/jsonparser v0.0.0-20191025224154-2951042f1c13
