package processors

const foobar1_out = "1600266221.005323\thello\tmy\t3.14000\t-"
const foobar1_in = `{
  "_path": "foobar",
  "ts": "2020-09-16T14:23:41.005323Z",
  "this": "hello",
  "that": "my",
  "the": 3.14
}`

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

const ssh1_out = "1600262948.560780\tCFb8DZ1DLzStfZaERb\t205.166.94.9\t55699\t192.168.4.37\t22\t-\t-\t0\tINBOUND\t-\tSSH-2.0-OpenSSH_7.6p1 Ubuntu-4ubuntu0.3\t-\t-\t-\t-\t-\t-\t-"
const ssh1_in = `{
  "_path": "ssh",
  "ts": "2020-09-16T13:29:08.560780Z",
  "uid": "CFb8DZ1DLzStfZaERb",
  "id.orig_h": "205.166.94.9",
  "id.orig_p": 55699,
  "id.resp_h": "192.168.4.37",
  "id.resp_p": 22,
  "auth_attempts": 0,
  "direction": "INBOUND",
  "server": "SSH-2.0-OpenSSH_7.6p1 Ubuntu-4ubuntu0.3"
}`

const ssh2_out = "1600262963.245216\tCzEmsljW9ooL0WnBd\t35.196.195.158\t53160\t192.168.4.37\t22\t2\ttrue\t1\tINBOUND\tSSH-2.0-OpenSSH_7.9p1 Debian-10+deb10u2\tSSH-2.0-OpenSSH_7.6p1 Ubuntu-4ubuntu0.3\tchacha20-poly1305@openssh.com\tumac-64-etm@openssh.com\tnone\tcurve25519-sha256\tecdsa-sha2-nistp256\ta3:41:03:32:1f:8c:8e:82:92:9f:62:8c:38:82:d3:74\t-"
const ssh2_in = `{
  "_path": "ssh",
  "ts": "2020-09-16T13:29:23.245216Z",
  "uid": "CzEmsljW9ooL0WnBd",
  "id.orig_h": "35.196.195.158",
  "id.orig_p": 53160,
  "id.resp_h": "192.168.4.37",
  "id.resp_p": 22,
  "version": 2,
  "auth_success": true,
  "auth_attempts": 1,
  "direction": "INBOUND",
  "client": "SSH-2.0-OpenSSH_7.9p1 Debian-10+deb10u2",
  "server": "SSH-2.0-OpenSSH_7.6p1 Ubuntu-4ubuntu0.3",
  "cipher_alg": "chacha20-poly1305@openssh.com",
  "mac_alg": "umac-64-etm@openssh.com",
  "compression_alg": "none",
  "kex_alg": "curve25519-sha256",
  "host_key_alg": "ecdsa-sha2-nistp256",
  "host_key": "a3:41:03:32:1f:8c:8e:82:92:9f:62:8c:38:82:d3:74",
  "hasshVersion": "1.0",
  "hassh": "ec7378c1a92f5a8dde7e8b7a1ddf33d1",
  "hasshServer": "b12d2871a1189eff20364cf5333619ee",
  "cshka": "ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa",
  "hasshAlgorithms": "curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1,ext-info-c;chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com;umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1;none,zlib@openssh.com,zlib",
  "sshka": "ssh-rsa,rsa-sha2-512,rsa-sha2-256,ecdsa-sha2-nistp256,ssh-ed25519",
  "hasshServerAlgorithms": "curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1;chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com;umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1;none,zlib@openssh.com"
}`

const ssh3_out = "1600261738.933098\tCjmfpo49s3lei7CBla\t192.168.4.49\t39550\t205.166.94.16\t22\t2\ttrue\t2\tOUTBOUND\tSSH-2.0-OpenSSH_7.4p1 Raspbian-10+deb9u7\tSSH-2.0-OpenSSH_8.0\tchacha20-poly1305@openssh.com\tumac-64-etm@openssh.com\tnone\tcurve25519-sha256\tssh-ed25519\te4:ff:65:d7:be:5d:c8:44:1d:89:6b:50:f5:50:a0:ce\t-"
const ssh3_in = `{
  "_path": "ssh",
  "ts": "2020-09-16T13:08:58.933098Z",
  "uid": "Cjmfpo49s3lei7CBla",
  "id.orig_h": "192.168.4.49",
  "id.orig_p": 39550,
  "id.resp_h": "205.166.94.16",
  "id.resp_p": 22,
  "version": 2,
  "auth_success": true,
  "auth_attempts": 2,
  "direction": "OUTBOUND",
  "client": "SSH-2.0-OpenSSH_7.4p1 Raspbian-10+deb9u7",
  "server": "SSH-2.0-OpenSSH_8.0",
  "cipher_alg": "chacha20-poly1305@openssh.com",
  "mac_alg": "umac-64-etm@openssh.com",
  "compression_alg": "none",
  "kex_alg": "curve25519-sha256",
  "host_key_alg": "ssh-ed25519",
  "host_key": "e4:ff:65:d7:be:5d:c8:44:1d:89:6b:50:f5:50:a0:ce",
  "hasshVersion": "1.0",
  "hassh": "0df0d56bb50c6b2426d8d40234bf1826",
  "hasshServer": "b12d2871a1189eff20364cf5333619ee",
  "cshka": "ssh-ed25519-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,rsa-sha2-512,rsa-sha2-256,ssh-rsa",
  "hasshAlgorithms": "curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group-exchange-sha1,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1,ext-info-c;chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com,aes128-cbc,aes192-cbc,aes256-cbc;umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1;none,zlib@openssh.com,zlib",
  "sshka": "ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa,ssh-ed25519",
  "hasshServerAlgorithms": "curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1;chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com;umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1;none,zlib@openssh.com"
}`

const ssh4_out = "1600266221.005323\tCOfRkd4UVXYwu1GTqh\t192.168.4.142\t57442\t192.168.4.1\t22\t2\t-\t0\t-\tSSH-2.0-OpenSSH_7.5\tSSH-2.0-OpenSSH_6.6.1p1 Debian-4~bpo70+1\taes128-ctr\thmac-md5\tzlib@openssh.com\tcurve25519-sha256@libssh.org\tssh-rsa\tf9:1f:45:88:dd:da:82:c5:7c:9d:75:c3:ac:e6:f4:f6\t-"
const ssh4_in = `{
  "_path": "ssh",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "COfRkd4UVXYwu1GTqh",
  "id.orig_h": "192.168.4.142",
  "id.orig_p": 57442,
  "id.resp_h": "192.168.4.1",
  "id.resp_p": 22,
  "version": 2,
  "auth_attempts": 0,
  "client": "SSH-2.0-OpenSSH_7.5",
  "server": "SSH-2.0-OpenSSH_6.6.1p1 Debian-4~bpo70+1",
  "cipher_alg": "aes128-ctr",
  "mac_alg": "hmac-md5",
  "compression_alg": "zlib@openssh.com",
  "kex_alg": "curve25519-sha256@libssh.org",
  "host_key_alg": "ssh-rsa",
  "host_key": "f9:1f:45:88:dd:da:82:c5:7c:9d:75:c3:ac:e6:f4:f6",
  "hasshVersion": "1.0",
  "hassh": "0d7f08c427fb41f68ec40fbe8fb7b5cb",
  "hasshServer": "b003da101c8caf37ce9e3ca3cd9d049b",
  "cshka": "ssh-rsa-cert-v01@openssh.com,ssh-rsa,ecdsa-sha2-nistp256-cert-v01@openssh.com,ssh-dss-cert-v01@openssh.com,ssh-dss,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519",
  "hasshAlgorithms": "curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group-exchange-sha1,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1,ext-info-c;aes128-ctr,aes192-ctr,aes256-ctr,arcfour256,arcfour128,aes256-gcm@openssh.com,aes128-cbc,3des-cbc,arcfour,aes128-gcm@openssh.com,chacha20-poly1305@openssh.com,blowfish-cbc,cast128-cbc,aes192-cbc,aes256-cbc,rijndael-cbc@lysator.liu.se;hmac-md5,hmac-sha1,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-ripemd160,hmac-sha1-96,hmac-md5-96,umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-md5-etm@openssh.com,hmac-sha1-etm@openssh.com,hmac-ripemd160-etm@openssh.com,hmac-sha1-96-etm@openssh.com,hmac-md5-96-etm@openssh.com,hmac-ripemd160@openssh.com;zlib@openssh.com,zlib,none",
  "sshka": "ssh-rsa,ssh-dss,ecdsa-sha2-nistp256,ssh-ed25519",
  "hasshServerAlgorithms": "curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group-exchange-sha1,diffie-hellman-group14-sha1,diffie-hellman-group1-sha1;chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com;hmac-md5-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-ripemd160-etm@openssh.com,hmac-sha1-96-etm@openssh.com,hmac-md5-96-etm@openssh.com,hmac-md5,hmac-sha1,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-ripemd160,hmac-ripemd160@openssh.com,hmac-sha1-96,hmac-md5-96;none,zlib@openssh.com"
}`

const http1_out = "1600266221.005323	C5bLoe2Mvxqhawzqqd	192.168.4.76	46378	31.3.245.133	80	1	GET	testmyids.com	/	-	1.1	curl/7.47.0	-	0	39	200	OK	-	-	[]	-	-	-	-	-	-	[FEEsZS1w0Z0VJIb5x4]	-	[text/plain]"
const http1_in = `{
  "_path": "http",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "C5bLoe2Mvxqhawzqqd",
  "id.orig_h": "192.168.4.76",
  "id.orig_p": 46378,
  "id.resp_h": "31.3.245.133",
  "id.resp_p": 80,
  "trans_depth": 1,
  "method": "GET",
  "host": "testmyids.com",
  "uri": "/",
  "version": "1.1",
  "user_agent": "curl/7.47.0",
  "request_body_len": 0,
  "response_body_len": 39,
  "status_code": 200,
  "status_msg": "OK",
  "tags": [],
  "resp_fuids": [
    "FEEsZS1w0Z0VJIb5x4"
  ],
  "resp_mime_types": [
    "text/plain"
  ]
}`

const files1_out = "1600266221.005323	FBbQxG1GXLXgmWhbk9	[23.195.64.241]	[192.168.4.37]	[CzoFRWTQ6YIzfFXHk]	HTTP	0	[EXTRACT PE]	application/x-dosexec	-	0.01550	-	false	179272	179272	0	0	false	-	-	-	-	HTTP-FBbQxG1GXLXgmWhbk9.exe	false	-"
const files1_in = `{
  "_path": "files",
  "ts": "2020-09-16T14:23:41.005323Z",
  "fuid": "FBbQxG1GXLXgmWhbk9",
  "tx_hosts": [
    "23.195.64.241"
  ],
  "rx_hosts": [
    "192.168.4.37"
  ],
  "conn_uids": [
    "CzoFRWTQ6YIzfFXHk"
  ],
  "source": "HTTP",
  "depth": 0,
  "analyzers": [
    "EXTRACT",
    "PE"
  ],
  "mime_type": "application/x-dosexec",
  "duration": 0.015498876571655273,
  "is_orig": false,
  "seen_bytes": 179272,
  "total_bytes": 179272,
  "missing_bytes": 0,
  "overflow_bytes": 0,
  "timedout": false,
  "extracted": "HTTP-FBbQxG1GXLXgmWhbk9.exe",
  "extracted_cutoff": false
}`

const ssl1_out = "1600266221.005323	CsukF91Bx9mrqdEaH9	192.168.4.49	56718	13.32.202.10	443	TLSv12	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256	secp256r1	www.taosecurity.com	false	-	h2	true	[F2XEvj1CahhdhtfvT4 FZ7ygD3ERPfEVVohG9 F7vklpOKI4yX9wmvh FAnbnR32nIIr2j9XV]	[]	CN=www.taosecurity.com	CN=Amazon,OU=Server CA 1B,O=Amazon,C=US	-	-	-"
const ssl1_in = `{
  "_path": "ssl",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CsukF91Bx9mrqdEaH9",
  "id.orig_h": "192.168.4.49",
  "id.orig_p": 56718,
  "id.resp_h": "13.32.202.10",
  "id.resp_p": 443,
  "version": "TLSv12",
  "cipher": "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
  "curve": "secp256r1",
  "server_name": "www.taosecurity.com",
  "resumed": false,
  "next_protocol": "h2",
  "established": true,
  "cert_chain_fuids": [
    "F2XEvj1CahhdhtfvT4",
    "FZ7ygD3ERPfEVVohG9",
    "F7vklpOKI4yX9wmvh",
    "FAnbnR32nIIr2j9XV"
  ],
  "client_cert_chain_fuids": [],
  "subject": "CN=www.taosecurity.com",
  "issuer": "CN=Amazon,OU=Server CA 1B,O=Amazon,C=US"
}`

const ssl2_out = "1600266221.005323	CcJfBs3hXLJn7oHVu7	192.168.4.142	58802	13.32.202.2	443	TLSv13	TLS_AES_128_GCM_SHA256	x25519	www.taosecurity.com	true	-	-	true	-	-	-	-	-	-	-"
const ssl2_in = `{
  "_path": "ssl",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CcJfBs3hXLJn7oHVu7",
  "id.orig_h": "192.168.4.142",
  "id.orig_p": 58802,
  "id.resp_h": "13.32.202.2",
  "id.resp_p": 443,
  "version": "TLSv13",
  "cipher": "TLS_AES_128_GCM_SHA256",
  "curve": "x25519",
  "server_name": "www.taosecurity.com",
  "resumed": true,
  "established": true
}`

const x5091_out = "1600266220.005323	F2XEvj1CahhdhtfvT4	3	0B58BC3898391F36592BA1BE1F6B03EF	CN=www.taosecurity.com	CN=Amazon,OU=Server CA 1B,O=Amazon,C=US	1590969600	1625140800	rsaEncryption	sha256WithRSAEncryption	rsa	2048	65537	-	[www.taosecurity.com taosecurity.com *.taosecurity.com]	-	-	-	false	-"
const x5091_in = `{
  "_path": "x509",
  "ts": "1600266220.005323",
  "id": "F2XEvj1CahhdhtfvT4",
  "certificate.version": 3,
  "certificate.serial": "0B58BC3898391F36592BA1BE1F6B03EF",
  "certificate.subject": "CN=www.taosecurity.com",
  "certificate.issuer": "CN=Amazon,OU=Server CA 1B,O=Amazon,C=US",
  "certificate.not_valid_before": 1590969600,
  "certificate.not_valid_after": 1625140800,
  "certificate.key_alg": "rsaEncryption",
  "certificate.sig_alg": "sha256WithRSAEncryption",
  "certificate.key_type": "rsa",
  "certificate.key_length": 2048,
  "certificate.exponent": "65537",
  "san.dns": [
    "www.taosecurity.com",
    "taosecurity.com",
    "*.taosecurity.com"
  ],
  "basic_constraints.ca": false
}`

const smtp1_out = `1254722768.219663	C1qe8w3QHRF2N5tVV5	10.10.1.4	1470	74.53.140.153	25	1	GP	gurpartap@patriots.in	[raj_deol2002in@yahoo.co.in]	Mon, 5 Oct 2009 11:36:07 +0530	"Gurpartap Singh" <gurpartap@patriots.in>	[<raj_deol2002in@yahoo.co.in>]	-	-	<000301ca4581$ef9e57f0$cedb07d0$@in>	-	SMTP	-	-	-	250 OK id=1Mugho-0003Dg-Un	[74.53.140.153 10.10.1.4]	Microsoft Office Outlook 12.0	false`
const smtp1_in = `{
  "_path": "smtp",
  "ts": "1254722768.219663",
  "uid": "C1qe8w3QHRF2N5tVV5",
  "id.orig_h": "10.10.1.4",
  "id.orig_p": 1470,
  "id.resp_h": "74.53.140.153",
  "id.resp_p": 25,
  "trans_depth": 1,
  "helo": "GP",
  "mailfrom": "gurpartap@patriots.in",
  "rcptto": [
    "raj_deol2002in@yahoo.co.in"
  ],
  "date": "Mon, 5 Oct 2009 11:36:07 +0530",
  "from": "\"Gurpartap Singh\" <gurpartap@patriots.in>",
  "to": [
    "<raj_deol2002in@yahoo.co.in>"
  ],
  "msg_id": "<000301ca4581$ef9e57f0$cedb07d0$@in>",
  "subject": "SMTP",
  "last_reply": "250 OK id=1Mugho-0003Dg-Un",
  "path": [
    "74.53.140.153",
    "10.10.1.4"
  ],
  "user_agent": "Microsoft Office Outlook 12.0",
  "tls": false,
  "fuids": [
    "Fel9gs4OtNEV6gUJZ5",
    "Ft4M3f2yMvLlmwtbq9",
    "FL9Y0d45OI4LpS6fmh"
  ]
}`

const pe1_out = `1600820676.395445	FGYKX64SkXc4OcvlFf	AMD64	2020-09-19T00:10:08.000000Z	Windows XP x64 or Server 2003	WINDOWS_GUI	true	true	true	true	false	true	true	true	true	[.text .rdata .data .pdata .00cfg .rsrc .reloc]`
const pe1_in = `{
  "_path": "pe",
  "ts": "2020-09-23T00:24:36.395445Z",
  "id": "FGYKX64SkXc4OcvlFf",
  "machine": "AMD64",
  "compile_ts": "2020-09-19T00:10:08.000000Z",
  "os": "Windows XP x64 or Server 2003",
  "subsystem": "WINDOWS_GUI",
  "is_exe": true,
  "is_64bit": true,
  "uses_aslr": true,
  "uses_dep": true,
  "uses_code_integrity": false,
  "uses_seh": true,
  "has_import_table": true,
  "has_export_table": false,
  "has_cert_table": true,
  "has_debug_data": true,
  "section_names": [
    ".text",
    ".rdata",
    ".data",
    ".pdata",
    ".00cfg",
    ".rsrc",
    ".reloc"
  ]
}`

const ntp1_out = `1600820676.395445	CqlPpF1AQVLMPgGiL5	192.168.4.49	38461	208.79.89.249	123	4	3	0	1	1	0	0	\x00\x00\x00\x00	1970-01-01T00:00:00.000000Z	1970-01-01T00:00:00.000000Z	1970-01-01T00:00:00.000000Z	2020-10-08T00:29:07.215586Z	0`
const ntp1_in = `{
  "_path": "ntp",
  "ts": "2020-09-23T00:24:36.395445Z",
  "uid": "CqlPpF1AQVLMPgGiL5",
  "id.orig_h": "192.168.4.49",
  "id.orig_p": 38461,
  "id.resp_h": "208.79.89.249",
  "id.resp_p": 123,
  "version": 4,
  "mode": 3,
  "stratum": 0,
  "poll": 1,
  "precision": 1,
  "root_delay": 0,
  "root_disp": 0,
  "ref_id": "\\x00\\x00\\x00\\x00",
  "ref_time": "1970-01-01T00:00:00.000000Z",
  "org_time": "1970-01-01T00:00:00.000000Z",
  "rec_time": "1970-01-01T00:00:00.000000Z",
  "xmt_time": "2020-10-08T00:29:07.215586Z",
  "num_exts": 0
}`

const ntp2_out = `1600820676.395445	CqlPpF1AQVLMPgGiL5	192.168.4.49	38461	208.79.89.249	123	4	4	2	8	0.00000	0.00922	0.02122	127.67.113.92	2020-10-08T00:24:15.942204Z	2020-10-08T00:29:07.215586Z	2020-10-08T00:29:07.964281Z	2020-10-08T00:29:07.964314Z	0`
const ntp2_in = `{
  "_path": "ntp",
  "ts": "2020-09-23T00:24:36.395445Z",
  "uid": "CqlPpF1AQVLMPgGiL5",
  "id.orig_h": "192.168.4.49",
  "id.orig_p": 38461,
  "id.resp_h": "208.79.89.249",
  "id.resp_p": 123,
  "version": 4,
  "mode": 4,
  "stratum": 2,
  "poll": 8,
  "precision": 5.960464477539063e-08,
  "root_delay": 0.00921630859375,
  "root_disp": 0.0212249755859375,
  "ref_id": "127.67.113.92",
  "ref_time": "2020-10-08T00:24:15.942204Z",
  "org_time": "2020-10-08T00:29:07.215586Z",
  "rec_time": "2020-10-08T00:29:07.964281Z",
  "xmt_time": "2020-10-08T00:29:07.964314Z",
  "num_exts": 0
}`

const notice1_out = `1600820676.395445	CxdbSa2KGTlMl3PPB2	192.168.4.129	51020	40.71.25.43	8080	FtEE2txjFBxLDbffi	-	-	tcp	SSL::Invalid_Server_Cert	SSL certificate validation failed with (unable to get local issuer certificate)	CN=*.cloudapp.net,OU=Smart Controller Development,O=GTO Access Systems\, LLC,DC=smartcontroller,DC=local	192.168.4.129	40.71.25.43	8080	-	so16-enp0s8-1	[Notice::ACTION_LOG]	3600	-	-	-	-	-	-`
const notice1_in = `{
  "_path": "notice",
  "ts": "2020-09-23T00:24:36.395445Z",
  "uid": "CxdbSa2KGTlMl3PPB2",
  "id.orig_h": "192.168.4.129",
  "id.orig_p": 51020,
  "id.resp_h": "40.71.25.43",
  "id.resp_p": 8080,
  "fuid": "FtEE2txjFBxLDbffi",
  "proto": "tcp",
  "note": "SSL::Invalid_Server_Cert",
  "msg": "SSL certificate validation failed with (unable to get local issuer certificate)",
  "sub": "CN=*.cloudapp.net,OU=Smart Controller Development,O=GTO Access Systems\\, LLC,DC=smartcontroller,DC=local",
  "src": "192.168.4.129",
  "dst": "40.71.25.43",
  "p": 8080,
  "peer_descr": "so16-enp0s8-1",
  "actions": [
    "Notice::ACTION_LOG"
  ],
  "suppress_for": 3600
}`

const notice2_out = `1600820676.395445	-	-	-	-	-	-	-	-	-	ATTACK::Discovery	Detected activity from host 192.168.10.31, total attempts 5 within timeframe 5.0 mins	-	-	-	-	-	-	[Notice::ACTION_LOG]	3600	-	-	-	-	-	-`
const notice2_in = `{
  "_path": "notice",
  "ts": "2020-09-23T00:24:36.395445Z",
  "note": "ATTACK::Discovery",
  "msg": "Detected activity from host 192.168.10.31, total attempts 5 within timeframe 5.0 mins",
  "actions": [
    "Notice::ACTION_LOG"
  ],
  "suppress_for": 3600
}`

const notice3_out = `1600820676.395445	CR7Vww4LuLkMzi4jMd	192.168.10.31	49238	192.168.10.30	445	FwVZpk12AKBjE11UNg	application/x-dosexec	temp	tcp	ATTACK::Lateral_Movement_Extracted_File	Saved a copy of the file written to SMB admin file share	CR7Vww4LuLkMzi4jMd_FwVZpk12AKBjE11UNg__admin-pc_c$temp_mimikatz.exe	192.168.10.31	192.168.10.30	445	-	-	[Notice::ACTION_LOG]	3600	-	-	-	-	-	-`
const notice3_in = `{
  "_path": "notice",
  "ts": "2020-09-23T00:24:36.395445Z",
  "uid": "CR7Vww4LuLkMzi4jMd",
  "id.orig_h": "192.168.10.31",
  "id.orig_p": 49238,
  "id.resp_h": "192.168.10.30",
  "id.resp_p": 445,
  "fuid": "FwVZpk12AKBjE11UNg",
  "file_mime_type": "application/x-dosexec",
  "file_desc": "temp",
  "proto": "tcp",
  "note": "ATTACK::Lateral_Movement_Extracted_File",
  "msg": "Saved a copy of the file written to SMB admin file share",
  "sub": "CR7Vww4LuLkMzi4jMd_FwVZpk12AKBjE11UNg__admin-pc_c$temp_mimikatz.exe",
  "src": "192.168.10.31",
  "dst": "192.168.10.30",
  "p": 445,
  "actions": [
    "Notice::ACTION_LOG"
  ],
  "suppress_for": 3600
}`

const weird1_out = `1600820676.395445	CxdbSa2KGTlMl3PPB2	192.168.4.129	51020	40.71.25.43	8080	bad_HTTP_request	-	false	so16-enp0s8-1	-`
const weird1_in = `{
  "_path": "weird",
  "ts": "2020-09-23T00:24:36.395445Z",
  "uid": "CxdbSa2KGTlMl3PPB2",
  "id.orig_h": "192.168.4.129",
  "id.orig_p": 51020,
  "id.resp_h": "40.71.25.43",
  "id.resp_p": 8080,
  "name": "bad_HTTP_request",
  "notice": false,
  "peer": "so16-enp0s8-1"
}`

const dpd1_out = "1600266221.005323	C8blOJ21azairPrWf8	192.168.4.142	50540	184.168.176.1	443	tcp	SSL	Invalid version late in TLS connection. Packet reported version: 21588	-"
const dpd1_in = `{
  "_path": "dpd",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "C8blOJ21azairPrWf8",
  "id.orig_h": "192.168.4.142",
  "id.orig_p": 50540,
  "id.resp_h": "184.168.176.1",
  "id.resp_p": 443,
  "proto": "tcp",
  "analyzer": "SSL",
  "failure_reason": "Invalid version late in TLS connection. Packet reported version: 21588"
}`

const irc1_out = "1600266221.005323	CDsHGC2ZJuJh10XNbk	192.168.4.142	52856	195.154.200.232	6667	zeektest	zeektest	JOIN	#freenode		-	-	-	-"
const irc1_in = `{
  "_path": "irc",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CDsHGC2ZJuJh10XNbk",
  "id.orig_h": "192.168.4.142",
  "id.orig_p": 52856,
  "id.resp_h": "195.154.200.232",
  "id.resp_p": 6667,
  "nick": "zeektest",
  "user": "zeektest",
  "command": "JOIN",
  "value": "#freenode",
  "addl": ""
}`

const rdp1_out = "1600266221.005323	CFdEZNjN5MtPzGMS8	192.168.4.160	59758	192.168.4.161	3389	test	encrypted	HYBRID	-	-	-	-	-	-	-	-	-	0	-	-	-"
const rdp1_in = `{
  "_path": "rdp",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CFdEZNjN5MtPzGMS8",
  "id.orig_h": "192.168.4.160",
  "id.orig_p": 59758,
  "id.resp_h": "192.168.4.161",
  "id.resp_p": 3389,
  "cookie": "test",
  "result": "encrypted",
  "security_protocol": "HYBRID",
  "cert_count": 0
}`

const kerberos1_out = "1600266221.005323	CT7qITytKtae83Tyi	192.168.10.31	49242	192.168.10.10	88	TGS	RonHD/CONTOSO.LOCAL	HOST/admin-pc	true	-	-	2136422885	aes256-cts-hmac-sha1-96	true	true	-	-	-	-"
const kerberos1_in = `{
  "_path": "kerberos",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CT7qITytKtae83Tyi",
  "id.orig_h": "192.168.10.31",
  "id.orig_p": 49242,
  "id.resp_h": "192.168.10.10",
  "id.resp_p": 88,
  "request_type": "TGS",
  "client": "RonHD/CONTOSO.LOCAL",
  "service": "HOST/admin-pc",
  "success": true,
  "till": 2136422885,
  "cipher": "aes256-cts-hmac-sha1-96",
  "forwardable": true,
  "renewable": true
}`

const smb_mapping1_out = `1600266221.005323	C6HQVsDf8VCu0XTJe	192.168.10.31	49239	192.168.10.30	445	\\admin-pc\ADMIN$	-	-	DISK`
const smb_mapping1_in = `{
  "_path": "smb_mapping",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "C6HQVsDf8VCu0XTJe",
  "id.orig_h": "192.168.10.31",
  "id.orig_p": 49239,
  "id.resp_h": "192.168.10.30",
  "id.resp_p": 445,
  "path": "\\\\admin-pc\\ADMIN$",
  "share_type": "DISK"
}`

const smb_files1_out = `1600266221.005323	C6HQVsDf8VCu0XTJe	192.168.10.31	49239	192.168.10.30	445	-	SMB::FILE_OPEN	\\admin-pc\ADMIN$	PSEXESVC.exe	0	-	1507565599.60778	1507565599.60778	1507565599.60778	1507565599.60778`
const smb_files1_in = `{
  "_path": "smb_files",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "C6HQVsDf8VCu0XTJe",
  "id.orig_h": "192.168.10.31",
  "id.orig_p": 49239,
  "id.resp_h": "192.168.10.30",
  "id.resp_p": 445,
  "action": "SMB::FILE_OPEN",
  "path": "\\\\admin-pc\\ADMIN$",
  "name": "PSEXESVC.exe",
  "size": 0,
  "times.modified": 1507565599.607777,
  "times.accessed": 1507565599.607777,
  "times.created": 1507565599.607777,
  "times.changed": 1507565599.607777
}`

const tunnel1_out = `1600266221.005323	CBvCtfO5sjjyQb2V4	16.0.0.200	0	192.52.166.154	0	Tunnel::GRE	Tunnel::DISCOVER`
const tunnel1_in = `{
  "_path": "tunnel",
  "ts": "2020-09-16T14:23:41.005323Z",
  "uid": "CBvCtfO5sjjyQb2V4",
  "id.orig_h": "16.0.0.200",
  "id.orig_p": 0,
  "id.resp_h": "192.52.166.154",
  "id.resp_p": 0,
  "tunnel_type": "Tunnel::GRE",
  "action": "Tunnel::DISCOVER"
}`

const software1_out = `1600266221.005323	192.168.4.25	-	HTTP::BROWSER	Windows-Update-Agent	10	0	10011	16384	Client	Windows-Update-Agent/10.0.10011.16384 Client-Protocol/2.0`
const software1_in = `{
  "_path": "software",
  "ts": "2020-09-16T14:23:41.005323Z",
  "host": "192.168.4.25",
  "software_type": "HTTP::BROWSER",
  "name": "Windows-Update-Agent",
  "version.major": 10,
  "version.minor": 0,
  "version.minor2": 10011,
  "version.minor3": 16384,
  "version.addl": "Client",
  "unparsed_version": "Windows-Update-Agent/10.0.10011.16384 Client-Protocol/2.0"
}`
