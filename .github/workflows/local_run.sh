#!/bin/bash

set -e

go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck -test -show verbose ./netflow/...
govulncheck -test -show verbose ./manager/...
govulncheck -test -show verbose ./ipexist/...
govulncheck -test -show verbose ./generators/ipgen/...
govulncheck -test -show verbose ./generators/base/...
govulncheck -test -show verbose ./generators/gravwellGenerator/...
govulncheck -test -show verbose ./filewatch/...
govulncheck -test -show verbose ./client/...
govulncheck -test -show verbose ./chancacher/...
govulncheck -test -show verbose ./timegrinder/...
govulncheck -test -show verbose ./ingest
govulncheck -test -show verbose ./ingest/entry/...
govulncheck -test -show verbose ./ingest/config/...
govulncheck -test -show verbose ./ingest/log
govulncheck -test -show verbose ./ingest/processors
govulncheck -test -show verbose ./ingest/processors/tags
govulncheck -test -show verbose ./ingest/processors/plugin
govulncheck -test -show verbose ./tools/...
govulncheck -test -show verbose ./kitctl/...
govulncheck -test -show verbose ./migrate/...
govulncheck -test -show verbose ./ingesters/s3Ingester
govulncheck -test -show verbose ./ingesters/HttpIngester
govulncheck -test -show verbose ./ingesters/pcapFileIngester
govulncheck -test -show verbose ./ingesters/collectd
govulncheck -test -show verbose ./ingesters/hackernews_ingester
govulncheck -test -show verbose ./ingesters/base
govulncheck -test -show verbose ./ingesters/massFile
govulncheck -test -show verbose ./ingesters/MSGraphIngester
govulncheck -test -show verbose ./ingesters/kafka_consumer
govulncheck -test -show verbose ./ingesters/reddit_ingester
govulncheck -test -show verbose ./ingesters/O365Ingester
govulncheck -test -show verbose ./ingesters/args
govulncheck -test -show verbose ./ingesters/version
govulncheck -test -show verbose ./ingesters/sqsIngester
govulncheck -test -show verbose ./ingesters/diskmonitor
govulncheck -test -show verbose ./ingesters/session
govulncheck -test -show verbose ./ingesters/snmp
govulncheck -test -show verbose ./ingesters/xlsxIngester
govulncheck -test -show verbose ./ingesters/multiFile
govulncheck -test -show verbose ./ingesters/Shodan
govulncheck -test -show verbose ./ingesters/reimport
govulncheck -test -show verbose ./ingesters/SimpleRelay
govulncheck -test -show verbose ./ingesters/KinesisIngester
govulncheck -test -show verbose ./ingesters/netflow
govulncheck -test -show verbose ./ingesters/AzureEventHubs
govulncheck -test -show verbose ./ingesters/utils
govulncheck -test -show verbose ./ingesters/IPMIIngester
govulncheck -test -show verbose ./ingesters/regexFile
govulncheck -test -show verbose ./ingesters/PacketFleet
govulncheck -test -show verbose ./ingesters/canbus
govulncheck -test -show verbose ./ingesters/GooglePubSubIngester
govulncheck -test -show verbose ./ingesters/fileFollow
govulncheck -test -show verbose ./ingesters/singleFile
GOOS=windows govulncheck -test -show verbose ./ingesters/winevents
GOOS=windows govulncheck -test -show verbose ./winevent/...

go test -v ./generators/ipgen
go test -v ./chancacher
go test -v ./ingest
go test -v ./ingest/entry
go test -v ./ingest/processors
go test -v ./ingest/processors/plugin
go test -v ./ingest/config
go test -v ./ingest/log
go test -v ./timegrinder
go test -v ./filewatch
go test -v ./ingesters/utils
go test -v ./ingesters/kafka_consumer
go test -v ./ingesters/SimpleRelay
go test -v ./ipexist
go test -v ./netflow
go test -v ./client/...

go build -o /dev/null ./generators/gravwellGenerator
go build -o /dev/null ./manager
go build -o /dev/null ./migrate
go build -o /dev/null ./tools/timetester
go build -o /dev/null ./timegrinder/cmd
go build -o /dev/null ./ipexist/textinput
go build -o /dev/null ./kitctl
GOOS=windows go build -o /dev/null ./ingesters/fileFollow
GOOS=windows go build -o /dev/null ./ingesters/winevents
GOOS=windows go build ./generators/windowsEventGenerator
go build -o /dev/null ./ingesters/massFile
go build -o /dev/null ./ingesters/diskmonitor
go build -o /dev/null ./ingesters/xlsxIngester
go build -o /dev/null ./ingesters/reimport
go build -o /dev/null ./ingesters/version
go build -o /dev/null ./ingesters/canbus
go build -o /dev/null ./ingesters/reddit_ingester
go build -o /dev/null ./ingesters/hackernews_ingester
go build -o /dev/null ./ingesters/multiFile
go build -o /dev/null ./ingesters/session
go build -o /dev/null ./ingesters/regexFile
go build -o /dev/null ./ingesters/Shodan
go build -o /dev/null ./ingesters/singleFile
go build -o /dev/null ./ingesters/pcapFileIngester
GOOS=darwin GOARCH=amd64 go build -o /dev/null ./ingesters/fileFollow
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./ingesters/fileFollow
GOOS=linux GOARCH=arm64 go build -o /dev/null ./ingesters/fileFollow

/bin/bash ./ingesters/test/build.sh ./ingesters/GooglePubSubIngester ./ingesters/test/configs/pubsub_ingest.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/AzureEventHubs ingesters/test/configs/azure_event_hubs.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/HttpIngester ingesters/test/configs/gravwell_http_ingester.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/collectd ingesters/test/configs/collectd.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/netflow ingesters/test/configs/netflow_capture.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/KinesisIngester ingesters/test/configs/kinesis_ingest.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/kafka_consumer ingesters/test/configs/kafka.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/MSGraphIngester ingesters/test/configs/msgraph_ingest.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/IPMIIngester ingesters/test/configs/ipmi.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/fileFollow ingesters/test/configs/file_follow.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/s3Ingester ingesters/test/configs/s3.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/snmp ingesters/test/configs/snmp.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/sqsIngester ingesters/test/configs/sqs.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/networkLog ingesters/test/configs/network_capture.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/SimpleRelay ingesters/test/configs/simple_relay.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/O365Ingester ingesters/test/configs/o365_ingest.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/PacketFleet ingesters/test/configs/packet_fleet.conf

