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
/dev/shm/ingester -config-overlays="" -config-file=/dev/shm/config.cfg -validate

set +e
#check that a UUID was NOT set
grep -q "Ingester-UUID=" /dev/shm/config.cfg
if [ "$?" == "0" ]; then
	echo "$DIR set the UUID on validate"
	exit 3
fi

set -e
/dev/shm/ingester -config-overlays="" -config-file=/dev/shm/config.cfg -validate-uuid-config
set +e
grep -q "Ingester-UUID=" /dev/shm/config.cfg
if [ "$?" != "0" ]; then
	echo "$DIR did NOT set the UUID on validate-uuid-config"
	exit 3
fi

set -e
