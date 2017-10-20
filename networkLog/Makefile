#!/bin/bash
SHELL=/bin/bash
TOOLS=$(shell dirname ~/gravwell/installer/.)/tools.sh
TBASE=~/gravwell/ingesters/networkLog/.
TROOT=~/gravwell/.
BASE=$(shell dirname $(TBASE))
ROOT=$(shell dirname $(TROOT))
INSTALLER_TOOLS=${BASE}/installer/tools.sh
CONFIG_FILE=${BASE}/network_capture.conf
BACKEND_UNIT_FILE=${BASE}/gravwell_network_capture.service
DATE=`/bin/date +%Y-%b-%d`
TEMP=/dev/shm/gravwell_installer
INSTALLER=/dev/shm/gravwell_network_capture_installer.sh
BINARY=/dev/shm/gravwell_network_capture
CRASH_REPORT=${ROOT}/crashLogUpload
CRASH_REPORT_NAME=crash_report
CRASH_REPORT_UNIT_FILE=${CRASH_REPORT}/crashlog.service
VERSION="0.2.7"

all: network_capture installer

network_capture:
	go build --ldflags "-s -w" -o ${BINARY}

installer: network_capture crashReport
	mkdir -p ${TEMP}
	cp ${BINARY} ${TEMP}/
	cp ${CONFIG_FILE} ${TEMP}/
	cp ${BACKEND_UNIT_FILE} ${TEMP}/
	cp ${CRASH_REPORT}/${CRASH_REPORT_NAME} ${TEMP}/${CRASH_REPORT_NAME}
	cp ${CRASH_REPORT_UNIT_FILE} ${TEMP}/crashlog.service
	md5sum ${TEMP}/* > ${TEMP}/md5s.txt
	tar cfz /dev/shm/package.tar.gz -C ${TEMP} .
	base64 /dev/shm/package.tar.gz > /dev/shm/package.base64
	cat ${BASE}/installer.sh | sed -e '/_TOOLS_INSERTION_POINT_/r/${TOOLS}' | \
		sed -e '/TAR_FILE_CONTENT/r/dev/shm/package.base64' | sed '/TAR_FILE_CONTENT/d' | \
		sed '/_TOOLS_INSERTION_POINT_/d' > ${INSTALLER}
	rm -f /dev/shm/package.base64
	rm -f /dev/shm/package.tar.gz
	rm -rf ${TEMP}	
	mv ${INSTALLER} /tmp/gravwell_network_capture_installer_$(VERSION).sh
	echo; echo; echo "Installer is ready at /tmp/gravwell_network_capture_installer_$(VERSION).sh"

crashReport:
	pushd ${CRASH_REPORT}; CGO_ENABLED=0 go build --ldflags "-s -w" -o ${CRASH_REPORT_NAME}; popd

clean:
	rm -f ${BINARY}
	rm -f ${INSTALLER}
	rm -rf ${TEMP}
