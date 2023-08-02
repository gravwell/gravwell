#!/bin/bash
DIR="$1"
CONFIG="$2"

if [ ! -d "$DIR" ]; then
	echo "Missing ingester build path"
	echo "$DIR is not a directory"
	exit -1
fi
if [ ! -f "$CONFIG" ]; then
	echo "$DIR Missing ingester test config path"
	exit -1
fi
set -e
echo -n "Testing $DIR  "
go build -o /dev/shm/ingester $DIR
cp $CONFIG /dev/shm/config.cfg
/dev/shm/ingester -config-overlays="" -config-file /dev/shm/config.cfg -validate

set +e
#check that a UUID was set
g=$(grep "Ingester-UUID=" /dev/shm/config.cfg)
if [ "$g" != "" ]; then
	echo "$DIR set the UUID on validate"
	exit 3
fi

set -e
