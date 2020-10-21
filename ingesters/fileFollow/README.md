# Gravwell File Follower Ingester

The Gravwell Windows File Follower ingester is designed to run as a system service and follow the text output of files.  The ingesters supports Windows 7 through Windows 10 and any modern Windows server distribution.  Both 32 and 64 bit builds are possible, but only the 64bit build is officially supported.

## Building the application

Build service using at least Go version 1.13, but we reccomend the latest go version.

`
CGO_ENABLED=0 GOARCH=amd64 GOOS=windows go build
`

## Building the installer

The installer is built using the [WIX Toolset](https://wixtoolset.org/).  To build an MSI, first build the winfilefollow.exe PE executable and then use the `build.bat` batch script to build an MSI.
