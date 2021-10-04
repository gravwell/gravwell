module github.com/gravwell/gravwell/v3

go 1.15

require (
	cloud.google.com/go/pubsub v1.3.1
	collectd.org v0.3.1-0.20181025072142-f80706d1e115
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/Shopify/sarama v1.24.1
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/asergeyev/nradix v0.0.0-20170505151046-3872ab85bb56
	github.com/aws/aws-sdk-go v1.29.11
	github.com/bet365/jingo v0.10.0
	github.com/buger/jsonparser v0.0.0-20191004114745-ee4c978eae7e
	github.com/bxcodec/faker/v3 v3.3.1
	github.com/cloudflare/buffer v0.0.0-20190408164202-7cab898e1166
	github.com/crewjam/rfc5424 v0.1.0 // indirect
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dvyukov/go-fuzz v0.0.0-20201127111758-49e582c6c23d // indirect
	github.com/eapache/go-resiliency v1.2.0 // indirect
	github.com/elastic/beats v7.6.2+incompatible
	github.com/elazarl/go-bindata-assetfs v1.0.1 // indirect
	github.com/floren/o365 v0.0.1
	github.com/fsnotify/fsnotify v1.5.1
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/gobwas/glob v0.2.3
	github.com/gofrs/flock v0.8.0
	github.com/golang-jwt/jwt v3.2.1+incompatible // indirect
	github.com/golang/snappy v0.0.1
	github.com/google/go-write v0.0.0-20181107114627-56629a6b2542
	github.com/google/gopacket v1.1.17
	github.com/google/renameio v0.1.0
	github.com/google/uuid v1.1.1
	github.com/gorilla/websocket v1.4.2
	github.com/gravwell/gcfg v1.2.9-0.20210818172109-3d05a45a2665
	github.com/gravwell/ipfix v1.4.3
	github.com/h2non/filetype v1.0.10
	github.com/inhies/go-bytesize v0.0.0-20201103132853-d0aed0d254f8
	github.com/jcmturner/gofork v1.0.0 // indirect
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901
	github.com/k-sone/ipmigo v0.0.0-20190922011749-b22c7a70e949
	github.com/klauspost/compress v1.11.12
	github.com/kr/pretty v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/minio/highwayhash v1.0.0
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/open-networks/go-msgraph v0.0.0-20200217121338-a7bf31e9c1f2
	github.com/open2b/scriggo v0.52.2 // indirect
	github.com/pierrec/lz4 v2.4.0+incompatible // indirect
	github.com/rcrowley/go-metrics v0.0.0-20190826022208-cac0b30c2563 // indirect
	github.com/shirou/gopsutil v2.20.9+incompatible
	github.com/stephens2424/writerset v1.0.2 // indirect
	github.com/stretchr/testify v1.5.1
	github.com/tealeg/xlsx v1.0.5
	github.com/turnage/graw v0.0.0-20191104042329-405cc3092119
	github.com/turnage/redditproto v0.0.0-20151223012412-afedf1b6eddb // indirect
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20201124201722-c8d3bf9c5392 // indirect
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c
	golang.org/x/text v0.3.3 // indirect
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1
	google.golang.org/protobuf v1.22.0 // indirect
	gopkg.in/jcmturner/gokrb5.v7 v7.3.0 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)

// Leave this until https://github.com/buger/jsonparser/pull/180 is merged
replace github.com/buger/jsonparser => github.com/floren/jsonparser v0.0.0-20210727191945-e5063027fceb

replace github.com/open-networks/go-msgraph => github.com/floren/go-msgraph v0.0.0-20200818171114-ec95909b54e3

// replace github.com/gravwell/gravwell/v3 => /home/kris/githubwork/gravwell

replace github.com/cloudflare/buffer => github.com/traetox/buffer v0.0.0-20210409000708-97e100fbf3c5
