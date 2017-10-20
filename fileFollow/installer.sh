#!/bin/sh

#temporary target directory
TEMP="/dev/shm/gravwell_installer"

_TOOLS_INSERTION_POINT_

CONFIG_FILE=$ETC/file_follow.conf
BINARY=$BIN/gravwell_file_follow
CACHE=$BASE_DIR/cache
CRASH_LOG=$BASE_DIR/log/crash
CRASH_REPORTER=$BIN/gravwell_crash_report
NO_ASK="no"
NO_START="no"
NO_CRASHREPORT="no"
ALT_CONFIG_FILE=""

printHelp() {
	echo "Installer options and flags:"
	echo
	echo "--no-questions - The installer assumes defaults and won't stop to ask questions"
	echo
	echo "--no-start - The installer will not start the service"
	echo
	echo "--use-config - The installer use the specified configuration file rather than the embedded config"
	echo
	echo "--no-crash-report - The crash report service will not be installed"
	echo
}

parseArgs() {
	for f in "$@"; do
		if [ "$use_config" == "true" ]; then
			echo "Using config file $f"
			ALT_CONFIG_FILE="$f"
			if [ ! -f "$ALT_CONFIG_FILE" ]; then
				echo "$f is not a valid configuration file"
				exit -1
			fi
			use_config="false"
			continue
		fi

		case $f in
		"--no-questions")
			NO_ASK="yes"
			echo "Assuming defaults, no questions will be asked"
			;;
		"--no-start")
			NO_START="yes"
			echo "Installer will not start the service"
			;;
		"--use-config")
			use_config="true"
			;;
		"--no-crash-report")
			NO_CRASHREPORT="true"
			;;
		"--help")
			printHelp
			exit 0
			;;
		*)
			printHelp
			exit 0
		esac
	done
}


installFiles() {
	mkdir -p $BIN
	mkdir -p $ETC
	mkdir -p $CACHE
	mkdir -p $CRASH_LOG
	mv $TEMP/gravwell_file_follow $BINARY
	if [ "$NO_CRASHREPORT" != "yes" ]; then
		mv $TEMP/crash_report $CRASH_REPORTER
	fi

	if [ "$ALT_CONFIG_FILE" != "" ]; then
		if [ ! -f "$ALT_CONFIG_FILE" ]; then
			echo "Configuration file $ALT_CONFIG_FILE not found"
			exit -1
		fi
		mv $ALT_CONFIG_FILE $CONFIG_FILE
	elif [ -f "$CONFIG_FILE" ]; then
		if [ "$NO_ASK" == "yes" ]; then
			return
		fi
		echo "An existing config file exists at $CONFIG_FILE"
		echo "Would you like to keep the existing configuration?"
		getYesOrNo
		if [ "$yesno" == "y" ]; then
			return
		fi
	fi
	mv $TEMP/file_follow.conf $CONFIG_FILE
}

stopService() {
	checkSystemD
	if [ "$SYSTEM_D_INSTALL" != "true" ]; then
		return
	fi
	if [ ! -f "/etc/systemd/system/gravwell_file_follow.service" ]; then
		return
	fi
	/bin/systemctl stop gravwell_file_follow.service 2>&1 > /dev/null
	/bin/systemctl disable gravwell_file_follow.service 2>&1 > /dev/null 
}

checkService() {
	checkSystemD
	if [ "$SYSTEM_D_INSTALL" != "true" ]; then
		return
	fi
	FAILED="no"
	if [ ! -f "/etc/systemd/system/gravwell_file_follow.service" ]; then
		/bin/systemctl status gravwell_file_follow.service 2>&1 > /dev/null
		if [ "$?" != "0" ]; then
			echo "The simple relay service is not running!"
			echo "Check your configuration file and and the systemd output"
			exit -1
		fi
	fi
}

installInit() {
	checkSystemD
	if [ "$SYSTEM_D_INSTALL" != "true" ]; then
		return
	fi

	#ok, copy the unit file over, register it, set it to start at boot, and go
	echo -n "Installing services... "
	if [ "$NO_CRASHREPORT" != "yes" ]; then
		mv $TEMP/crashlog.service /etc/systemd/system/gravwell_crash_report@.service
		/bin/systemctl enable gravwell_crash_report@.service 2>&1 > /dev/null
		#Do not start this service it is invoked by OnFail on other services
	fi

	mv $TEMP/gravwell_file_follow.service /etc/systemd/system/gravwell_file_follow.service
	/bin/systemctl enable gravwell_file_follow.service 2>&1 > /dev/null 
	if [ "$NO_START" != "yes" ]; then
		/bin/systemctl start gravwell_file_follow.service
	fi
	echo "DONE"
}

checkAndDropPackage() {
	if [ -f "$TEMP/package.tar.bz2" ]; then
		echo "An installer already exists at $TEMP/installer.tar.bz2"
		echo "Please verify that another installer isn't running and delete $TEMP"
		abort
	fi
	dropFiles
	checkPackageContents
}

checkPackageContents() {
	if [ ! -f "$TEMP/md5s.txt" ]; then
		echo "The installer package has been corrupted."
		echo "Please contact support and request a new installer"
		abort
	fi
	md5sum -c $TEMP/md5s.txt 2>&1 > /dev/null
	if [ "$?" != "0" ]; then
		echo "The embedded package failed to verify"
		noticeAndBail
	fi
}

extractIngestSecret() {
	if [ ! -f "$ETC/gravwell.conf" ]; then
		echo; echo; echo
		echo "*******  We could not find a backend resident on this device  *******"
		echo
		echo "If you do not want to enter your Ingest-Auth value, just press enter"
		read -p "Otherwise, input your Ingest-Auth secret and press enter: " key
		if [ "$authVal" == "" ]; then
			echo
			echo "*********************************************************************"
			echo "   You must set the Ingest-Auth VALUE IN $CONFIG_FILE"
			echo "   The value Ingest-Secret value MUST match the indexers "
			echo "   Restart the gravwell_file_follow service afterwords"
			echo "*********************************************************************"
			echo
			return
		fi
	else
		#we have one, go get it
		key=`cat $ETC/gravwell.conf | grep Ingest-Auth | sed 's/=/ /g' | awk '{print $2}'`
	fi
	sed -i "s/IngestSecrets/$key/g" $CONFIG_FILE
}

setRelayOwnership() {
	chown $USER:$GROUP $CONFIG_FILE
	chmod o-rwx $CONFIG_FILE
	chown $USER:$GROUP $BINARY
	chmod o-rwx $BINARY
	chown $USER:$GROUP $CACHE
	chmod o-rwx $CACHE
	chown $USER:$GROUP $CRASH_LOG
	chmod o-rwx $CRASH_LOG
}

dropFiles() {
mkdir -p $TEMP
(cd $TEMP && base64 -d | tar -xzf -) << 'ENDOFFILE'
TAR_FILE_CONTENT
ENDOFFILE
}

main() {
	parseArgs $@
	checkPermissions
	checkTools
	checkAndDropUserGroup "noask"
	stopService
	checkAndDropPackage
	installFiles
	extractIngestSecret
	setRelayOwnership
	installInit
	rm -rf $TEMP
	checkService
	echo; echo;
	echo "Installation complete.  Thank you for using GravWell!"
	echo
}

main $@
