## Ingest Preprocessor Tester

The plugintest program provides a simple scaffolding for testing ingest preprocessor stacks, it is designed to accept a data export from Gravwell and run the raw data through a set of preprocesors without actually ingesting any data.

### Getting Started

First you will need to get a raw data export from some unprocessed data, this can be a simple text file that is line delimited or a JSON export of data from Gravwell.  For example, if we were working with syslog data from the `syslog` tag we might run the following query:

```
tag=syslog limit 1000 | raw
```

### Testing Preprocessors

An example stack of preprocessors may have a configuration file like so:

```
[Global]
	Preprocessor = apprtr
	Preprocessor=loginapp

[Preprocessor "apprtr"]
    Type = syslogrouter
    Template=`syslog-${Appname}`


[Preprocessor "sshattach"]
    Type=regexextract
	Regex=`Failed password for( invalid user)? (?P<user>\w+) from (?P<ip>\S+)`
	Template=`${_DATA_}`
	Attach=user
	Attach=ip
```

The example config is executing a [syslogrouter](https://docs.gravwell.io/ingesters/preprocessors/syslogrouter.html) preprocessor followed by a [regexattach](https://docs.gravwell.io/ingesters/preprocessors/regexextract.html) preprocessor.  The calling order is defined in the `[Global]` section.


```
#> ./test --help
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
