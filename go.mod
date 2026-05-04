module github.com/gravwell/gravwell/v4

go 1.26.2

require (
	cloud.google.com/go/pubsub/v2 v2.5.1
	collectd.org v0.5.0
	github.com/Azure/azure-amqp-common-go/v3 v3.2.3
	github.com/Azure/azure-event-hubs-go/v3 v3.3.18
	github.com/Bowery/prompt v0.0.0-20190916142128-fa8279994f75
	github.com/IBM/sarama v1.45.1
	github.com/Jeffail/gabs/v2 v2.7.0
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/asergeyev/nradix v0.0.0-20170505151046-3872ab85bb56
	github.com/aws/aws-sdk-go v1.55.7
	github.com/bmatcuk/doublestar/v4 v4.4.0
	github.com/brianvoe/gofakeit v3.18.0+incompatible
	github.com/bxcodec/faker/v3 v3.3.1
	github.com/charmbracelet/bubbles v0.21.0
	github.com/charmbracelet/bubbletea v1.3.5
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/charmbracelet/x/ansi v0.8.0
	github.com/charmbracelet/x/exp/teatest v0.0.0-20250603201427-c31516f43444
	github.com/charmbracelet/x/term v0.2.1
	github.com/crewjam/rfc5424 v0.1.0
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/duosecurity/duo_api_golang v0.0.0-20250128191753-8aff7fde9979
	github.com/evertras/bubble-table v0.17.1
	github.com/fsnotify/fsnotify v1.7.0
	github.com/gdamore/tcell/v2 v2.6.1-0.20231203215052-2917c3801e73
	github.com/gobwas/glob v0.2.3
	github.com/goccy/go-json v0.10.2
	github.com/gofrs/flock v0.12.1
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/gopacket v1.1.19
	github.com/google/renameio v1.0.1
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.4.2
	github.com/gosnmp/gosnmp v1.35.0
	github.com/gravwell/buffer v0.0.0-20220728204757-23339f4bab66
	github.com/gravwell/gcfg v1.2.10-0.20251024173739-a5ba5db9985f
	github.com/gravwell/gravwell/v3 v3.8.73
	github.com/gravwell/ipfix v1.4.6-0.20240221191955-c76630f7cc37
	github.com/gravwell/ipmigo v0.0.0-20230307161134-29dce87c333e
	github.com/gravwell/jsonparser v0.0.0-20241122184510-d9e3b05f5d3b
	github.com/gravwell/o365 v0.0.0-20221102220049-82dbf0fa81b4
	github.com/gravwell/syslogparser v0.0.0-20250904221952-6d38d4266dee
	github.com/h2non/filetype v1.1.3
	github.com/inhies/go-bytesize v0.0.0-20201103132853-d0aed0d254f8
	github.com/jaswdr/faker/v2 v2.3.2
	github.com/k-sone/ipmigo v0.0.0-20190922011749-b22c7a70e949
	github.com/klauspost/compress v1.18.5
	github.com/magefile/mage v1.15.0
	github.com/miekg/dns v1.1.56
	github.com/minio/highwayhash v1.0.3
	github.com/open-networks/go-msgraph v0.3.1
	github.com/open2b/scriggo v0.56.1
	github.com/pquerna/otp v1.5.0
	github.com/rivo/tview v0.0.0-20240118093911-742cf086196e
	github.com/shirou/gopsutil v2.20.9+incompatible
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	github.com/stretchr/testify v1.11.1
	github.com/tealeg/xlsx v1.0.5
	github.com/turnage/graw v0.0.0-20191104042329-405cc3092119
	github.com/xdg-go/scram v1.1.2
	go.etcd.io/bbolt v1.4.3
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa
	golang.org/x/net v0.53.0
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.43.0
	golang.org/x/term v0.42.0
	golang.org/x/text v0.36.0
	golang.org/x/time v0.15.0
	google.golang.org/api v0.276.0
	google.golang.org/grpc v1.80.0
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.8.0 // indirect
	github.com/Azure/azure-sdk-for-go v68.0.0+incompatible // indirect
	github.com/Azure/go-amqp v0.17.0 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.30 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.24 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.12 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.1 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.1 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.2 // indirect
	github.com/Azure/go-autorest/logger v0.2.2 // indirect
	github.com/Azure/go-autorest/tracing v0.6.1 // indirect
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymanbagabas/go-udiff v0.2.0 // indirect
	github.com/boombuler/barcode v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/exp/golden v0.0.0-20241011142426-46044092ad91 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/devigned/tab v0.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20230731223053-c322873962e3 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gdamore/encoding v1.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.21.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20201227073835-cf1acfcdf475 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sahilm/fuzzy v0.1.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/turnage/redditproto v0.0.0-20151223012412-afedf1b6eddb // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	google.golang.org/genproto v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/gcfg.v1 v1.2.3 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
