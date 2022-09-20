package processors

const ftp1_out = "1597559163.553\tCLkXf2CMo11hD8FQ5\t192.168.4.76\t53380\t196.216.2.24\t21\tanonymous\tftp@example.com\tEPSV\t-\t-\t-\t229\tEntering Extended Passive Mode (|||31746|).\ttrue\t192.168.4.76\t196.216.2.24\t31746"
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
