#!/bin/bash

set -e

# get things all staged up
go mod tidy
go mod download
go mod verify
go install golang.org/x/vuln/cmd/govulncheck@latest
go install honnef.co/go/tools/cmd/staticcheck@latest

echo "Running go vet"
	go vet ./generators/ipgen
        go vet ./chancacher
        go vet ./ingest
        go vet ./ingest/entry
        go vet ./ingest/processors
        go vet ./ingest/processors/plugin
        go vet ./ingest/config
        go vet ./ingest/log
        go vet ./timegrinder
        go vet ./filewatch
        go vet ./ingesters/utils
        go vet ./ingesters/kafka_consumer
        go vet ./ingesters/SimpleRelay
        go vet ./ipexist
        go vet ./netflow
        go vet ./client/...


echo "Running go test -v"
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

echo "running staticcheck"
        staticcheck ./chancacher/...
        staticcheck ./debug/...
        staticcheck ./manager/...
	staticcheck ./generators/base/... ./generators/gravwellGenerator/... ./generators/ipgen/...
	GOOS=windows staticcheck ./generators/windowsEventGenerator/...
	staticcheck ./ingest
	staticcheck ./ingest/attach
	staticcheck ./ingest/config/...
	staticcheck ./ingest/log/...
	#staticcheck ./ingest/entry
	#staticcheck ./ingest/processors/...
	#staticcheck ./ingest/processors/plugin


echo "running govluncheck on everything"
        govulncheck -test ./netflow/...
        govulncheck -test ./manager/...
        govulncheck -test ./ipexist/...
        govulncheck -test ./generators/ipgen/...
        govulncheck -test ./generators/base/...
        govulncheck -test ./generators/gravwellGenerator/...
        govulncheck -test ./filewatch/...
        govulncheck -test ./client/...
        govulncheck -test ./chancacher/...
        govulncheck -test ./timegrinder/...
        govulncheck -test ./ingest
        govulncheck -test ./ingest/entry/...
        govulncheck -test ./ingest/config/...
        govulncheck -test ./ingest/log
        govulncheck -test ./ingest/processors
        govulncheck -test ./ingest/processors/tags
        govulncheck -test ./ingest/processors/plugin
        govulncheck -test ./tools/...
        govulncheck -test ./kitctl/...
        govulncheck -test ./migrate/...
        govulncheck -test ./ingesters/s3Ingester
        govulncheck -test ./ingesters/HttpIngester
        govulncheck -test ./ingesters/pcapFileIngester
        govulncheck -test ./ingesters/collectd
        govulncheck -test ./ingesters/hackernews_ingester
        govulncheck -test ./ingesters/base
        govulncheck -test ./ingesters/massFile
        govulncheck -test ./ingesters/MSGraphIngester
        govulncheck -test ./ingesters/kafka_consumer
        govulncheck -test ./ingesters/reddit_ingester
        govulncheck -test ./ingesters/O365Ingester
        govulncheck -test ./ingesters/args
        govulncheck -test ./ingesters/version
        govulncheck -test ./ingesters/sqsIngester
        govulncheck -test ./ingesters/diskmonitor
        govulncheck -test ./ingesters/session
        govulncheck -test ./ingesters/snmp
        govulncheck -test ./ingesters/xlsxIngester
        govulncheck -test ./ingesters/multiFile
        govulncheck -test ./ingesters/Shodan
        govulncheck -test ./ingesters/reimport
        govulncheck -test ./ingesters/SimpleRelay
        govulncheck -test ./ingesters/KinesisIngester
        govulncheck -test ./ingesters/netflow
        govulncheck -test ./ingesters/AzureEventHubs
        govulncheck -test ./ingesters/utils
        govulncheck -test ./ingesters/IPMIIngester
        govulncheck -test ./ingesters/regexFile
        govulncheck -test ./ingesters/PacketFleet
        govulncheck -test ./ingesters/canbus
        govulncheck -test ./ingesters/GooglePubSubIngester
        govulncheck -test ./ingesters/fileFollow
        govulncheck -test ./ingesters/singleFile
        govulncheck -test ./gwcli
        GOOS=windows govulncheck -test ./ingesters/winevents
        GOOS=windows govulncheck -test ./winevent/...


echo "Running build tests"
        go build -o /dev/null ./generators/gravwellGenerator
        go build -o /dev/null ./manager
        go build -o /dev/null ./migrate
        go build -o /dev/null ./tools/timetester
        go build -o /dev/null ./timegrinder/cmd
        go build -o /dev/null ./ipexist/textinput
        go build -o /dev/null ./kitctl
        go build -o /dev/null ./gwcli
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
