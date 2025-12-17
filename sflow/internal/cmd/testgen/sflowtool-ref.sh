#!/usr/bin/env bash
#
# sflowtool-ref.sh - Generate sflowtool JSON reference output for .bin test files
#
# This script is meant to be run AFTER the testgen binary, which captures raw
# sFlow packets as .bin files. This script then generates corresponding .json
# files using the official sflowtool (via Docker) to decode each packet.
#
# The resulting .json files serve as reference output to validate our Go
# sFlow decoder against the canonical sflowtool implementation.
#
# Usage:
#   bash sflowtool-ref.sh <tests_dir> [port]
#
# Arguments:
#   tests_dir  Directory containing .bin files (required)
#   port       Host UDP port to map to sflowtool (default: 6344)
#
# Requirements:
#   - Docker
#   - netcat (nc)
#   - jq (for JSON filtering)
#
# For each .bin file in the tests directory, this script:
# 1. Starts sflowtool in a Docker container listening on UDP
# 2. Sends the raw sFlow packet to sflowtool via UDP
# 3. Captures sflowtool's JSON output
# 4. Filters out sflowtool metadata fields (datagramSourceIP, datagramSize, 
#    unixSecondsUTC, unixSecondsUTC_uS, localtime) that are not part of 
#    the sFlow datagram specification
# 5. Writes the filtered JSON to a .json file with the same base name
#

set -e

TESTS_DIR="${1:-}"
HOST_PORT="${2:-6344}"
IMAGE="sflow/sflowtool:latest"

# Check tests directory argument
if [[ -z "$TESTS_DIR" ]]; then
    echo "ERROR: tests_dir argument is required" >&2
    echo "Usage: bash sflowtool-ref.sh <tests_dir> [port]" >&2
    exit 1
fi

# Check tests directory exists
if [[ ! -d "$TESTS_DIR" ]]; then
    echo "ERROR: Tests directory not found: $TESTS_DIR" >&2
    exit 1
fi

# Count .bin files
bin_files=("$TESTS_DIR"/*.bin)
if [[ ! -e "${bin_files[0]}" ]]; then
    echo "No .bin files found in $TESTS_DIR" >&2
    exit 0
fi

echo "Found ${#bin_files[@]} .bin file(s) in $TESTS_DIR" >&2
echo "Sending packets to UDP port $HOST_PORT" >&2

# Check Docker is available
if ! command -v docker &>/dev/null; then
    echo "ERROR: docker not found in PATH" >&2
    exit 1
fi

# Check netcat is available
if ! command -v nc &>/dev/null; then
    echo "ERROR: nc not found in PATH" >&2
    exit 1
fi

# Check jq is available
if ! command -v jq &>/dev/null; then
    echo "ERROR: jq not found in PATH" >&2
    exit 1
fi

# Pull the sflowtool image
echo "Pulling $IMAGE..." >&2
if ! docker pull "$IMAGE" >&2; then
    echo "ERROR: Failed to pull $IMAGE" >&2
    exit 1
fi

# Set up a FIFO for capturing sflowtool output.
#
# Why a FIFO? It's streaming - each read consumes data, so we get only new
# output after each packet without tracking file offsets.
#
# Why fd 3? FIFOs block writers until a reader opens the other end. By opening
# the FIFO read-write on fd 3 *before* starting docker, we ensure docker's
# writes don't block. Docker writes to fd 3, we read from fd 3, and the FIFO
# buffers between them.
FIFO=$(mktemp -u)
mkfifo "$FIFO"
trap 'rm -f "$FIFO"; docker rm -f sflowtool-ref 2>/dev/null || true' EXIT
exec 3<>"$FIFO"

# Start sflowtool container in background, outputting to fd 3
docker run --rm --name sflowtool-ref \
    -p "${HOST_PORT}:6343/udp" \
    "$IMAGE" -p 6343 -J \
    >&3 2>&1 &
DOCKER_PID=$!

# Give sflowtool a moment to start listening
sleep 1

# Verify the container is running
if ! docker ps --format '{{.Names}}' | grep -q '^sflowtool-ref$'; then
    echo "ERROR: sflowtool container failed to start" >&2
    exit 1
fi

echo "sflowtool container started (PID $DOCKER_PID)" >&2

# Process each .bin file
for bin in "${bin_files[@]}"; do
    basename="${bin%.bin}"
    json="${basename}.json"
    name="$(basename "$basename")"
    
    echo -n "Processing $name... " >&2
    
    # Send the raw packet as UDP datagram using nc
    cat "$bin" | nc -u -w1 127.0.0.1 "$HOST_PORT"
    
    # Read whatever sflowtool outputs in the next second
    output=""
    while IFS= read -r -t 1 line <&3; do
        if [[ -n "$output" ]]; then
            output+=$'\n'
        fi
        output+="$line"
    done
    
    if [[ -n "$output" ]]; then
        # Filter out sflowtool metadata fields that are not part of the sFlow datagram
        filtered=$(echo "$output" | jq 'del(.datagramSourceIP, .datagramSize, .unixSecondsUTC, .unixSecondsUTC_uS, .localtime)')
        echo "$filtered" > "$json"
        echo "wrote $json" >&2
    else
        echo "no output (timeout?)" >&2
    fi
done

echo "Done!" >&2
