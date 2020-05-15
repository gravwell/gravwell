This is a slightly tweaked version of the the Elastic winevent package which can interact with the windows Event subsystem.

The code is licensed under Apache 2.0

See https://github.com/elastic/beats/tree/master/winlogbeat/sys for the base code.

We have cleaned up some go vet issues and reworked some of the XML rendering code to be faster under the nominal case.
