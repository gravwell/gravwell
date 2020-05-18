# manager
Simple daemon manager for use in docker containers.

This is a lightweight process manager primarily used for docker.  It fires up N number of processes and restarts them upon exit with backoffs.

There is an optional "crash" handler which is fired if a process fails.

Its basically a very limited systemd like process manager that allows for crash reporting and restarts.

## Example Config

here is an example config which demonstrates setting the log level, an error handler, and firing a few processes.  This is the default config shipped in the Gravwell docker container.

```
[Global]
	Log-File=/opt/gravwell/log/manager.log
	Log-Level=INFO

[Error-Handler]
	Exec=/opt/gravwell/bin/crashReport

[Process "indexer"]
	Exec="/opt/gravwell/bin/gravwell_indexer -config-override /opt/gravwell/etc/gravwell.conf -stderr indexer"
	Working-Dir=/opt/gravwell
	Max-Restarts=3 #three attempts before cooling down
	CoolDown-Period=60 #1 hour
	Restart-Period=10 #10 minutes

[Process "webserver"]
	Exec="/opt/gravwell/bin/gravwell_webserver -config-override /opt/gravwell/etc/gravwell.conf -stderr webserver"
	Working-Dir=/opt/gravwell
	Max-Restarts=3 #three attempts before cooling down
	CoolDown-Period=30 #30 minutes
	Restart-Period=10 #10 minutes

[Process "searchagent"]
	Exec="/opt/gravwell/bin/gravwell_searchagent -config-override /opt/gravwell/etc/searchagent.conf -stderr searchagent"
	Working-Dir=/opt/gravwell
	Max-Restarts=3 #three attempts before cooling down
	CoolDown-Period=10 #10 minutes
	Restart-Period=10 #10 minutes

[Process "simple_relay"]
	Exec="/opt/gravwell/bin/gravwell_simple_relay -config-file /opt/gravwell/etc/simple_relay.conf -stderr simple_relay"
	Working-Dir=/opt/gravwell
	Max-Restarts=3 #three attempts before cooling down
	CoolDown-Period=10 #10 minutes
	Restart-Period=10 #10 minutes
```
