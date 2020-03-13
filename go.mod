module github.com/gravwell/ingesters/v3

go 1.13

require (
	cloud.google.com/go v0.49.0 // indirect
	cloud.google.com/go/pubsub v1.1.0
	cloud.google.com/go/storage v1.1.0 // indirect
	collectd.org v0.3.1-0.20181025072142-f80706d1e115
	github.com/Shopify/sarama v1.24.1
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/aws/aws-sdk-go v1.25.46
	github.com/buger/jsonparser v0.0.0-20191004114745-ee4c978eae7e
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/eapache/go-resiliency v1.2.0 // indirect
	github.com/etcd-io/bbolt v1.3.3 // indirect
	github.com/floren/ipfix v1.4.1
	github.com/floren/o365 v0.0.1
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/golang/groupcache v0.0.0-20191027212112-611e8accdfc9 // indirect
	github.com/google/go-cmp v0.3.1 // indirect
	github.com/google/gopacket v1.1.17
	github.com/google/uuid v1.1.1
	github.com/gravwell/filewatch/v3 v3.3.3
	github.com/gravwell/ingest/v3 v3.3.7
	github.com/gravwell/netflow/v3 v3.2.3
	github.com/gravwell/timegrinder/v3 v3.2.3
	github.com/gravwell/winevent/v3 v3.3.7
	github.com/h2non/filetype v1.0.10
	github.com/jcmturner/gofork v1.0.0 // indirect
	github.com/jstemmer/go-junit-report v0.9.1 // indirect
	github.com/klauspost/compress v1.9.3 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/pierrec/lz4 v2.3.0+incompatible // indirect
	github.com/rcrowley/go-metrics v0.0.0-20190826022208-cac0b30c2563 // indirect
	github.com/shirou/gopsutil v2.19.11+incompatible
	github.com/tealeg/xlsx v1.0.5
	github.com/turnage/graw v0.0.0-20191104042329-405cc3092119
	github.com/turnage/redditproto v0.0.0-20151223012412-afedf1b6eddb // indirect
	go.opencensus.io v0.22.2 // indirect
	golang.org/x/crypto v0.0.0-20191202143827-86a70503ff7e // indirect
	golang.org/x/exp v0.0.0-20191129062945-2f5052295587 // indirect
	golang.org/x/lint v0.0.0-20191125180803-fdd1cda4f05f // indirect
	golang.org/x/net v0.0.0-20191126235420-ef20fe5d7933 // indirect
	golang.org/x/oauth2 v0.0.0-20191202225959-858c2ad4c8b6 // indirect
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e // indirect
	golang.org/x/sys v0.0.0-20200219091948-cb0a6d8edb6c
	golang.org/x/tools v0.0.0-20191203233240-b1451cf3445b // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/genproto v0.0.0-20191203220235-3fa9dbf08042 // indirect
	google.golang.org/grpc v1.25.1 // indirect
	gopkg.in/gcfg.v1 v1.2.3
	gopkg.in/jcmturner/gokrb5.v7 v7.3.0 // indirect
)

//replace github.com/gravwell/winevent/v3 => /opt/src/githubwork/winevent // for debugging
//replace github.com/gravwell/ingest/v3 => /opt/src/githubwork/ingest // for debugging
