package processors

const conn1_out = `1597559163.553287	CMdzit1AMNsmfAIiQc	192.168.4.76	36844	192.168.4.1	53	udp	dns	0.06685	62	141	SF	-	-	0	Dd	2	118	2	197	-	-`
const conn1_in = `{
  "_path": "conn",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "CMdzit1AMNsmfAIiQc",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 36844,
  "id.resp_h": "192.168.4.1",
  "id.resp_p": 53,
  "proto": "udp",
  "service": "dns",
  "duration": 0.06685185432434082,
  "orig_bytes": 62,
  "resp_bytes": 141,
  "conn_state": "SF",
  "missed_bytes": 0,
  "history": "Dd",
  "orig_pkts": 2,
  "orig_ip_bytes": 118,
  "resp_pkts": 2,
  "resp_ip_bytes": 197
}`

const conn2_out = `1597559163.553287	C5bLoe2Mvxqhawzqqd	192.168.4.76	46378	31.3.245.133	80	tcp	http	0.25412	77	295	SF	-	-	0	ShADadFf	6	397	4	511	-	-`
const conn2_in = `{
  "_path": "conn",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "C5bLoe2Mvxqhawzqqd",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 46378,
  "id.resp_h": "31.3.245.133",
  "id.resp_p": 80,
  "proto": "tcp",
  "service": "http",
  "duration": 0.25411510467529297,
  "orig_bytes": 77,
  "resp_bytes": 295,
  "conn_state": "SF",
  "missed_bytes": 0,
  "history": "ShADadFf",
  "orig_pkts": 6,
  "orig_ip_bytes": 397,
  "resp_pkts": 4,
  "resp_ip_bytes": 511
}`

const dns1_out = `1597559163.553287	CMdzit1AMNsmfAIiQc	192.168.4.76	36844	192.168.4.1	53	udp	8555	-	testmyids.com	1	C_INTERNET	28	AAAA	0	NOERROR	false	false	true	false	0	-	-	false`
const dns1_in = `{
  "_path": "dns",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "CMdzit1AMNsmfAIiQc",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 36844,
  "id.resp_h": "192.168.4.1",
  "id.resp_p": 53,
  "proto": "udp",
  "trans_id": 8555,
  "query": "testmyids.com",
  "qclass": 1,
  "qclass_name": "C_INTERNET",
  "qtype": 28,
  "qtype_name": "AAAA",
  "rcode": 0,
  "rcode_name": "NOERROR",
  "AA": false,
  "TC": false,
  "RD": true,
  "RA": false,
  "Z": 0,
  "rejected": false
}`

const dns2_out = `1597559163.553287	CMdzit1AMNsmfAIiQc	192.168.4.76	36844	192.168.4.1	53	udp	19671	0.06685	testmyids.com	1	C_INTERNET	1	A	0	NOERROR	false	false	true	true	0	[31.3.245.133]	[3600]	false`
const dns2_in = `{
  "_path": "dns",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "CMdzit1AMNsmfAIiQc",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 36844,
  "id.resp_h": "192.168.4.1",
  "id.resp_p": 53,
  "proto": "udp",
  "trans_id": 19671,
  "rtt": 0.06685185432434082,
  "query": "testmyids.com",
  "qclass": 1,
  "qclass_name": "C_INTERNET",
  "qtype": 1,
  "qtype_name": "A",
  "rcode": 0,
  "rcode_name": "NOERROR",
  "AA": false,
  "TC": false,
  "RD": true,
  "RA": true,
  "Z": 0,
  "answers": [
    "31.3.245.133"
  ],
  "TTLs": [
    3600
  ],
  "rejected": false
}`

const dhcp1_out = `1597559163.553287	[COoA8M1gbTowuPlVT CapFoX32zVg3R6TATc]	192.168.4.152	192.168.4.1	3c:58:c2:2f:91:21	3071N0098017422	3071N0098017422.fcps.edu	localdomain	192.168.4.152	192.168.4.152	86400	-	-	[DISCOVER OFFER REQUEST ACK]	0.41635`
const dhcp1_in = `{
  "_path": "dhcp",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uids": [
    "COoA8M1gbTowuPlVT",
    "CapFoX32zVg3R6TATc"
  ],
  "client_addr": "192.168.4.152",
  "server_addr": "192.168.4.1",
  "mac": "3c:58:c2:2f:91:21",
  "host_name": "3071N0098017422",
  "client_fqdn": "3071N0098017422.fcps.edu",
  "domain": "localdomain",
  "requested_addr": "192.168.4.152",
  "assigned_addr": "192.168.4.152",
  "lease_time": 86400,
  "msg_types": [
    "DISCOVER",
    "OFFER",
    "REQUEST",
    "ACK"
  ],
  "duration": 0.416348934173584
}
`

const ftp1_out = "1597559163.553287\tCLkXf2CMo11hD8FQ5\t192.168.4.76\t53380\t196.216.2.24\t21\tanonymous\tftp@example.com\tEPSV\t-\t-\t-\t229\tEntering Extended Passive Mode (|||31746|).\ttrue\t192.168.4.76\t196.216.2.24\t31746\t-"
const ftp1_in = `{
  "_path": "ftp",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:04.077276Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:03.553287Z",
  "uid": "CLkXf2CMo11hD8FQ5",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 53380,
  "id.resp_h": "196.216.2.24",
  "id.resp_p": 21,
  "user": "anonymous",
  "password": "ftp@example.com",
  "command": "EPSV",
  "reply_code": 229,
  "reply_msg": "Entering Extended Passive Mode (|||31746|).",
  "data_channel.passive": true,
  "data_channel.orig_h": "192.168.4.76",
  "data_channel.resp_h": "196.216.2.24",
  "data_channel.resp_p": 31746
}`

const ftp2_out = "1597559164.597290\tCLkXf2CMo11hD8FQ5\t192.168.4.76\t53380\t196.216.2.24\t21\tanonymous\tftp@example.com\tRETR\tftp://196.216.2.24/pub/stats/afrinic/delegated-afrinic-extended-latest.md5\t-\t74\t226\tTransfer complete.\t-\t-\t-\t-\tFueF95uKPrUuDnMc4"
const ftp2_in = `{
  "_path": "ftp",
  "_system_name": "ds61",
  "_write_ts": "2020-08-16T06:26:05.117287Z",
  "_node": "worker-01",
  "ts": "2020-08-16T06:26:04.597290Z",
  "uid": "CLkXf2CMo11hD8FQ5",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 53380,
  "id.resp_h": "196.216.2.24",
  "id.resp_p": 21,
  "user": "anonymous",
  "password": "ftp@example.com",
  "command": "RETR",
  "arg": "ftp://196.216.2.24/pub/stats/afrinic/delegated-afrinic-extended-latest.md5",
  "file_size": 74,
  "reply_code": 226,
  "reply_msg": "Transfer complete.",
  "fuid": "FueF95uKPrUuDnMc4"
}`
