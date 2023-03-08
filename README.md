[![Go Reference](https://pkg.go.dev/badge/github.com/gravwell/gravwell/v3.svg)](https://pkg.go.dev/github.com/gravwell/gravwell/v3)

# Gravwell Open-Source Code

This repository contains open-sourced libraries and commands developed by [Gravwell](https://gravwell.io).

There are a selection of Gravwell-specific libraries and tools:

* `ingest/` contains the [ingest library](https://pkg.go.dev/github.com/gravwell/gravwell/v3/ingest?tab=doc), which is used to connect to a Gravwell indexer and upload data.
* `ingesters/` contains the source code for Gravwell ingesters.
* `generators/` is a collection of tools that generate artificial data for testing Gravwell or any other log analytics system.
* `manager/` provides a very simple init command which we use in Docker containers.
* `chancacher/` implements a caching library we use for ingesters.

There are also a few libraries which may be of use outside Gravwell-specific applications:

* `filewatch/` is a library that can monitor files on the filesystem for changes; we use this in the FileFollow ingester.
* `timegrinder/` is a [timestamp extraction library](https://pkg.go.dev/github.com/gravwell/gravwell/v3/timegrinder) we use to extract timestamps from arbitrary data
* `ipexist/` contains a library for efficiently storing and checking for the existence of an IPv4 set with high density sets.
* `winevent/` is a library which can interact with the Windows Event subsystem to extract XML rendered events.
