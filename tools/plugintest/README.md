## Ingest Plugin Tester

The plugintest program provides a simple scaffolding for testing ingest plugins, it is designed to accept a data export from Gravwell and run the raw data through a set of preprocesors and plugins without actually ingesting any data.

### Getting Started

First you will need to get a raw data export from some unprocessed data, this can be a simple text file that is line delimited or a JSON export of data from Gravwell.  For example, if we were working with several `corelight` tags we might run the following query:

```
tag=corelight* limit 1000 | raw
```

### Testing A Plugin

An example plugin configuration file for a test plugin might be:

```
[Preprocessor "case_adjust"]
    Type=plugin
    Plugin-Path=/tmp/recase.go
    Upper=true
```

The plugin under test is located at the absolute path of `/tmp/recase.go`.  Build the `plugintest` application by executing `go build` in the plugin test directory.

The available set of flags for the `plugintest` is printed when the `--help` flag is provided:

```
#> ./plugintest --help
Usage of ./plugintest:
  -config-path string
    	Path to the plugin configuration
  -data-path string
    	Optional path to data export file
  -import-format string
    	Set the import file format manually
  -verbose
    	Print each entry as its processed
```

An example execution of the test plugin is:

```


#> ./plugintest -data-path /tmp/51780259054.json -config-path /tmp/recase.conf

```
INPUT: 100
OUTPUT: 100
PROCESSING TIME: 251.725404ms
PROCESSING RATE: 397.26 E/s
```

Adding the `--verbose` flag will cause the `plugintest` program to print every entry; if entries are not printable characters you may see garbage on the screen.

The `plugintest` program also enables debug mode for plugins by default, so any `printf` or `println` calls will output to standard out.
