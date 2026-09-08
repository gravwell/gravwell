#!/bin/bash

# Single entry point for running everything CI runs, locally.
# Mirrors .github/workflows/golang-testing.yml -- if you change one, change the other.

set -e

# Terminal colors, suppressed when stdout is not a TTY so piping to a log stays clean.
if [ -t 1 ]; then
	PURPLE=$'\033[1;35m'
	GREEN=$'\033[1;32m'
	RESET=$'\033[0m'
else
	PURPLE='' GREEN='' RESET=''
fi

section() { echo "${PURPLE}$*${RESET}"; }

# We deploy with cgo disabled, so everything is checked that way by default.
export CGO_ENABLED=0

# ingesters/networkLog is the sole exception: it pulls in gopacket/pcap, which
# needs libpcap via cgo. It is excluded from the CGO_ENABLED=0 sweeps below and
# gets its own CGO_ENABLED=1 pass. Requires libpcap-dev to be installed.
CGO_PKG='./ingesters/networkLog/...'

# Packages that cannot be built or tested on a native Linux host. These are
# covered separately via the GOOS/GOARCH cross-checks below.
NON_NATIVE='/(winevent|experiments|windowsEventGenerator|test_data)'
WINDOWS_PKGS='./winevent/... ./ingesters/winevents/... ./generators/windowsEventGenerator/...'

NATIVE=$(go list ./... 2>/dev/null | grep -vE "$NON_NATIVE" | grep -v '/ingesters/networkLog$')
# gwcli is tested separately with -tags ci, which is a superset of the untagged run
NATIVE_NO_GWCLI=$(echo "$NATIVE" | grep -v '/gwcli')

section "Skipping native vet/test/build for"
go list ./... 2>/dev/null | grep -E "$NON_NATIVE"

section "Installing tooling"
go install golang.org/x/vuln/cmd/govulncheck@latest
go install honnef.co/go/tools/cmd/staticcheck@latest

section "Checking module hygiene"
go mod tidy && git diff --exit-code go.mod go.sum
go mod download
go mod verify

section "Running go vet"
go vet $NATIVE
GOOS=windows go vet $WINDOWS_PKGS
CGO_ENABLED=1 go vet $CGO_PKG

section "Running staticcheck"
staticcheck $NATIVE
GOOS=windows staticcheck $WINDOWS_PKGS
CGO_ENABLED=1 staticcheck $CGO_PKG

section "Running govulncheck"
govulncheck -test $NATIVE
CGO_ENABLED=1 govulncheck -test $CGO_PKG

section "Running govulncheck on windows and non-native ingesters"
GOOS=windows govulncheck -test ./ingesters/winevents
GOOS=windows govulncheck -test ./winevent/...
GOOS=darwin GOARCH=amd64 govulncheck -test ./ingesters/fileFollow
GOOS=darwin GOARCH=arm64 govulncheck -test ./ingesters/fileFollow
GOOS=linux GOARCH=arm64 govulncheck -test ./ingesters/fileFollow

section "Running go test and excluding windows and experiments"
go test -p 4 $NATIVE_NO_GWCLI
go test -p 4 -tags ci ./gwcli/...
CGO_ENABLED=1 go test -p 4 $CGO_PKG

section "Building native components"
go build $NATIVE
CGO_ENABLED=1 go build -o /dev/null ./ingesters/networkLog

section "Building Windows, Darwin, and non-native components"
GOOS=windows go build -o /dev/null ./ingesters/fileFollow
GOOS=windows go build -o /dev/null ./ingesters/winevents
GOOS=windows go build -o /dev/null ./generators/windowsEventGenerator
GOOS=darwin GOARCH=amd64 go build -o /dev/null ./ingesters/fileFollow
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./ingesters/fileFollow
GOOS=linux GOARCH=arm64 go build -o /dev/null ./ingesters/fileFollow

section "Performing a test build and check with config"
/bin/bash ./ingesters/test/build.sh ./ingesters/GooglePubSubIngester ingesters/test/configs/pubsub_ingest.conf
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
/bin/bash ./ingesters/test/build.sh ./ingesters/SimpleRelay ingesters/test/configs/simple_relay.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/O365Ingester ingesters/test/configs/o365_ingest.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/PacketFleet ingesters/test/configs/packet_fleet.conf
/bin/bash ./ingesters/test/build.sh ./ingesters/llm_ingester ingesters/test/configs/llm_ingester.conf
CGO_ENABLED=1 /bin/bash ./ingesters/test/build.sh ./ingesters/networkLog ingesters/test/configs/network_capture.conf

echo "${GREEN}All checks passed${RESET}"
