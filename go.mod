module github.com/gravwell/generators/v3

go 1.13

require (
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/bet365/jingo v0.9.0
	github.com/etcd-io/bbolt v1.3.3 // indirect
	github.com/google/uuid v1.1.1
	github.com/gravwell/ingest v3.2.2+incompatible // indirect
	github.com/gravwell/ingest/v3 v3.2.3
	github.com/gravwell/ingesters v3.2.2+incompatible // indirect
	github.com/gravwell/timegrinder v3.2.2+incompatible // indirect
	github.com/h2non/filetype v1.0.10 // indirect
	golang.org/x/sys v0.0.0-20190919044723-0c1ff786ef13
	golang.org/x/text v0.3.2 // indirect
)

//replace github.com/gravwell/ingest/v3 => /opt/src/githubwork/ingest // for debugging
