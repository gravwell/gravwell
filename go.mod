module github.com/gravwell/ingest/v3

go 1.13

require (
	github.com/buger/jsonparser v0.0.0-20191004114745-ee4c978eae7e
	github.com/google/go-write v0.0.0-20181107114627-56629a6b2542
	github.com/google/renameio v0.1.0 // indirect
	github.com/google/uuid v1.1.1
	github.com/gravwell/gcfg v1.2.5
	github.com/gravwell/timegrinder/v3 v3.2.5
	github.com/klauspost/compress v1.8.6
	github.com/klauspost/cpuid v1.2.1 // indirect
	github.com/minio/highwayhash v1.0.0
	go.etcd.io/bbolt v1.3.3
	golang.org/x/sys v0.0.0-20190919044723-0c1ff786ef13 // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	gopkg.in/gcfg.v1 v1.2.3 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
)

// Leave this until https://github.com/buger/jsonparser/pull/180 is merged
replace github.com/buger/jsonparser => github.com/floren/jsonparser v0.0.0-20191025224154-2951042f1c13
