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
