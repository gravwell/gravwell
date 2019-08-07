# Gravwell Windows Events Ingester

The Gravwell Windows events ingester is designed to run as a system service and collect Events from the Windows Event log.  The ingesters supports Windows 7 through Windows 10 and any modern Windows server distribution.  Both 32 and 64 bit builds are possible, but only the 64bit build is officially supported.

## Building the application

Build service using at least Go version 1.11, but we reccomend the latest go version.

`
CGO_ENABLED=0 GOARCH=386 GOOS=windows go build
`

## Building the installer

The installer is built using the [go-msi](https://github.com/mh-cbon/go-msi) system and [WiX](https://wixtoolset.org/).  You will need both tools installed.

Once the applications have been built, execute the go-msi command to build the MSI installer.

`
go-msi.exe make --version 3.2.0 --arch amd64 --msi gravwell_win_events_3.2.0.msi --src templates
`
