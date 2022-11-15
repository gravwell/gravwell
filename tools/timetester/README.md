## Time Tester

The purpose of this tool is to test [TimeGrinder](https://pkg.go.dev/github.com/gravwell/gravwell/v3/timegrinder) against log files.  The Time Tester tool can operate in one of two ways:

* Basic Mode
* Custom Timestamp Mode


### Basic Mode
Basic mode simply shows which timestamp extraction will match a given log line and where in the log line it matched.

It will show each log line with the timestamp capture location highlighted in red, the extracted timestamp, and the extraction name that hit.


### Custom Timestamp Mode

The custom timestamp mode operates the same as the basic mode but also accepts a path to custom timestamp declerations which allows you to test custom timestamps and also see how collisions or misses affect the TimeGrinder.


## Usage

Time Tester will walk each string provided on the command line and attempt to process it as if it were a timestamp.

The tester can set the timegrinder config values and define custom timestamp formats in the same way that Gravwell ingesters can.

```
./timetester -h
Usage of ./timetester:
  -custom string
        Path to custom time format configuration file
  -enable-left-most-seed
        Activate EnableLeftMostSeed config option
  -format-override string
        Enable FormatOverride config option
```

Here is an example of a custom time format that adds a year to the Syslog format:
```
[TimeFormat "syslog2"]
        Format=`Jan _2 15:04:05 2006`
        Regex=`[JFMASOND][anebriyunlgpctov]+\s+\d+\s+\d\d:\d\d:\d\d\s\d{4}`
```

#### Single Log Entry Test
Here is an example invocation using the basic mode and testing a single entry:

```
timetester "2022/05/27 12:43:45 server.go:233: the wombat hit a curb"
```


Results:
```
2022/05/27 12:43:46 server.go:233: the wombat hit a curb
	2022-05-27 12:43:46 +0000 UTC	NGINX
```

**NOTE:** Terminals capable of handling ANSI color codes will highlight the timestamp location in the log in green.

#### Multiple Log Entry Test
Here is an example that tests 3 entries in succession showing how different extractors operated.
First we use a custom time format from a custom application, then a zeek connection log, then back to the custom time format.  This shows that timegrinder will fail on the existing format then find a new format then go back to the old format.

```
timetester "2022/05/27 12:43:45 server.go:141: the wombat can't jump" \
	"1654543200.411042	CUgreS31Jc2Elmtph5	1.1.1.1	38030	2.2.2.2	23" \
	"2022/05/27 12:43:46 server.go:233: the wombat hit a curb"
```

Results:
```
2022/05/27 12:43:46 server.go:233: the wombat hit a curb
	2022-05-27 12:43:46 +0000 UTC	NGINX
1654543200.411042      CUgreS31Jc2Elmtph5      1.1.1.1 38030   2.2.2.2 23
	2022-06-06 19:20:00.411041975 +0000 UTC	UnixMilli
2022/05/27 12:43:46 server.go:233: the wombat hit a curb
	2022-05-27 12:43:46 +0000 UTC	NGINX
```a

### Caveats

The TimeGrinder object is re-used across each test, this is to simulate a single TimeGrinder object that is being used to process a string of logs on a given listener.  The Timegrinder system is designed to "lock on to" a given timestamp format and continue re-using it.  This means that if a log format misses but another hits, the TimeGrinder will continue using the format that "hit".

For example, ff you input the following three values:
```
1984-10-26 12:22:31 T-800 "I'm a friend of Sarah Connor. I was told she was here. Could I see her please?"
1991-6-1 12:22:31.453213 1991 T-1000 "Are you the legal guardian of John Connor?"
2004-7-25 22:18:24 Connor "It's not everyday you find out that you're responsible for three billion deaths. He took it pretty well."
```

The system will correctly interpret the first timestamp and lock onto the `UnpaddedDateTime` format, it would then see that the second line (minus the millisecond) also matches the UnpaddedDateTime format and use it, ignoring the millisecond components.  This is an artifact of the how the timegrinder optimizes its throughput by assuming that contiguous entries will be of the same format.
