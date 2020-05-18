#!/bin/sh
trap 'previous_command=$this_command; this_command=$BASH_COMMAND' DEBUG
trap errPrint ERR

origpwd=$PWD

errPrint(){
	echo Error running $previous_command
	echo bailing out
	cd $origpwd
}

if [ $# -ne 1 ]; then
    echo "Usage: update-versions.sh <version>"
    exit -1
fi

version=$1
release=`echo $version | sed "s/-.*$//"`

major=`echo $release | cut -d "." -f 1`
minor=`echo $release | cut -d "." -f 2`
point=`echo $release | cut -d "." -f 3`
date=`/bin/date +%Y-%b-%d`

echo $major $minor $point
igstVer="version/version.go"
if [ -f "$igstVer" ]; then
	year=$(date +%Y)
	mon=$(date +%m | sed -e "s/^0//")
	day=$(date +%d |  sed -e "s/^0//")
	echo "Updating $igstVer"
	sed -i -E "s/MajorVersion\s+=\s+.+/MajorVersion = ${major}/g" $igstVer
	sed -i -E "s/MinorVersion\s+=\s+.+/MinorVersion = ${minor}/g" $igstVer
	sed -i -E "s/PointVersion\s+=\s+.+/PointVersion = ${point}/g" $igstVer
	#update the date
	sed -i -E "s/BuildDate\s+time.Time\s+=.+/BuildDate time.Time = time.Date($year, $mon, $day, 0, 0, 0, 0, time.UTC)/g" $igstVer
fi
