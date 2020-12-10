module github.com/gravwell/gravwell/v3

go 1.13

require (
	cloud.google.com/go/pubsub v1.3.1
	collectd.org v0.3.1-0.20181025072142-f80706d1e115
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/Shopify/sarama v1.24.1
	github.com/aws/aws-sdk-go v1.29.11
	github.com/bet365/jingo v0.10.0
	github.com/buger/jsonparser v0.0.0-20191004114745-ee4c978eae7e
	github.com/bxcodec/faker/v3 v3.3.1
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/eapache/go-resiliency v1.2.0 // indirect
	github.com/elastic/beats v7.6.2+incompatible
	github.com/floren/o365 v0.0.1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/gobwas/glob v0.2.3
	github.com/gofrs/flock v0.8.0
	github.com/golang/snappy v0.0.1
	github.com/google/go-write v0.0.0-20181107114627-56629a6b2542
	github.com/google/gopacket v1.1.17
	github.com/google/renameio v0.1.0
	github.com/google/uuid v1.1.1
	github.com/gravwell/gcfg v1.2.8
	github.com/gravwell/ipfix v1.4.3
	github.com/h2non/filetype v1.0.10
	github.com/jcmturner/gofork v1.0.0 // indirect
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901
	github.com/klauspost/compress v1.11.3
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/minio/highwayhash v1.0.0
	github.com/minio/minio v0.0.0-20201209163743-e65ed2e44fdd
	github.com/open-networks/go-msgraph v0.0.0-20200217121338-a7bf31e9c1f2
	github.com/rcrowley/go-metrics v0.0.0-20190826022208-cac0b30c2563 // indirect
	github.com/shirou/gopsutil v2.20.9+incompatible
	github.com/stretchr/testify v1.5.1
	github.com/tealeg/xlsx v1.0.5
	github.com/turnage/graw v0.0.0-20191104042329-405cc3092119
	github.com/turnage/redditproto v0.0.0-20151223012412-afedf1b6eddb // indirect
	go.etcd.io/bbolt v1.3.5
	golang.org/x/sys v0.0.0-20200915084602-288bc346aa39
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1
)

// Leave this until https://github.com/buger/jsonparser/pull/180 is merged
replace github.com/buger/jsonparser => github.com/floren/jsonparser v0.0.0-20200807143944-7168565e7e04

replace github.com/open-networks/go-msgraph => github.com/floren/go-msgraph v0.0.0-20200818171114-ec95909b54e3

// replace github.com/gravwell/gravwell/v3 => /home/kris/githubwork/gravwell
