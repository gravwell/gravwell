#!/bin/bash

# Runs go fix over every .go file in the repo, across every GOOS/GOARCH/tag
# combination needed to type-check them all.
#
#   go_fix.sh diff      fail if any modernizer would change code
#   go_fix.sh fix       apply the changes
#   go_fix.sh coverage  fail if any .go file is not type-checked by a sweep
#
# The sweeps are defined once, below, so the drift check and the coverage
# check cannot disagree. Expects CGO_ENABLED, NATIVE, CGO_PKG and WINDOWS_PKGS
# in the environment, as set up by golang-testing.yml and local_run_checks.sh.

set -euo pipefail

MODE=${1:?usage: $0 diff|fix|coverage}
: "${CGO_ENABLED:?}" "${NATIVE:?}" "${CGO_PKG:?}" "${WINDOWS_PKGS:?}"

LIST_FILES='{{$d := .Dir}}{{range .GoFiles}}{{$d}}/{{.}}
{{end}}{{range .CgoFiles}}{{$d}}/{{.}}
{{end}}{{range .TestGoFiles}}{{$d}}/{{.}}
{{end}}{{range .XTestGoFiles}}{{$d}}/{{.}}
{{end}}'

# Packages holding files that GOOS=$1 GOARCH=$2 builds but linux/amd64 does not
# (_windows.go files, 32-bit stubs, and so on).
only_on() {
	comm -13 <(GOOS=linux GOARCH=amd64 go list -e -f "$LIST_FILES" ./... | sort) \
		<(GOOS=$1 GOARCH=$2 go list -e -f "$LIST_FILES" ./... | sort) \
		| xargs -n1 dirname | sort -u | sed "s|^$PWD|.|"
}

# sweep [-C dir] [VAR=value...] [-tags list] packages...
sweep() {
	local dir='' envs='' tags=''
	if [ "$1" = -C ]; then dir="-C $2"; shift 2; fi
	while [[ $1 == *=* ]]; do envs="$envs $1"; shift; done
	if [ "$1" = -tags ]; then tags="-tags $2"; shift 2; fi
	# An empty package list would make go default to the current directory.
	[ $# -gt 0 ] || return 0
	case $MODE in
	diff) env $envs go $dir fix -diff $tags "$@" ;;
	fix) env $envs go $dir fix $tags "$@" ;;
	coverage) env $envs go $dir list -e -f "$LIST_FILES" $tags "$@" ;;
	*) echo "unknown mode $MODE" >&2; exit 2 ;;
	esac
}

sweeps() {
	# experiments/ is kept out of NATIVE (and so out of vet/test/build).
	sweep GOOS=linux GOARCH=amd64 $NATIVE ./experiments/...
	sweep CGO_ENABLED=1 $CGO_PKG
	sweep GOOS=windows GOARCH=amd64 $WINDOWS_PKGS $(only_on windows amd64)
	sweep GOOS=darwin GOARCH=amd64 $(only_on darwin amd64)
	sweep GOOS=linux GOARCH=386 $(only_on linux 386)
	# gwcli splits its tests between ci (no backend) and noci (live backend).
	sweep GOOS=linux GOARCH=amd64 -tags ci ./gwcli/...
	sweep GOOS=linux GOARCH=amd64 -tags noci,integration ./gwcli/...
	sweep GOOS=linux GOARCH=amd64 -tags mage ./gwcli/magefiles
	sweep GOOS=linux GOARCH=amd64 -tags realsocket,gofuzz ./ingest ./ingest/processors
	# Separate modules.
	sweep -C e2e GOOS=linux GOARCH=amd64 ./...
	sweep -C tools/mock/mimecast GOOS=linux GOARCH=amd64 ./...
}

if [ "$MODE" != coverage ]; then
	sweeps
	exit
fi

# The test_data plugins are Scriggo scripts, not Go packages: they import a
# "gravwell" package that only exists inside the interpreter, and go fix would
# modernize them past what Scriggo can run.
missed=$(comm -23 <(find . -name '*.go' -not -path './.git/*' \
	-not -path './ingest/processors/test_data/plugins/*' | sort) \
	<(sweeps | sed "s|^$PWD/|./|" | sort -u))
if [ -n "$missed" ]; then
	echo "These .go files are not type-checked by any go fix sweep in $0:"
	echo "$missed"
	exit 1
fi
