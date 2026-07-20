/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	rd "github.com/Pallinder/go-randomdata"
)

// Implements the ArcSight Common Event Format (CEF) as described in
// "Implementing ArcSight Common Event Format" rev 25.
//
//	CEF:Version|Device Vendor|Device Product|Device Version|Device Event Class ID|Name|Severity|[Extension]
//
// Header fields escape backslash and pipe, extension values escape
// backslash, equals, and newlines.  Extension keys are restricted to the
// key names called out in the CEF extension dictionary.

const (
	cefVersion         = 0
	cefSyslogTsFormat  = `Jan _2 15:04:05` // CEF transport examples use BSD syslog style headers
	cefMaxSeveritySkew = 3
)

type cefEventClass struct {
	vendor   string
	product  string
	version  string
	classID  string
	name     string
	severity int // base severity 0 - 10, skewed downward at generation time
	ext      func(ts time.Time) string
}

var cefEventClasses = []cefEventClass{
	{
		vendor: `Gravwell`, product: `threatmanager`, version: `1.0`,
		classID: `100`, name: `worm successfully stopped`, severity: 10,
		ext: cefExtWorm,
	},
	{
		vendor: `Gravwell`, product: `firewall`, version: `2.3`,
		classID: `600`, name: `connection blocked`, severity: 6,
		ext: cefExtFirewall,
	},
	{
		vendor: `Gravwell`, product: `webgateway`, version: `5.1`,
		classID: `302`, name: `request processed`, severity: 2,
		ext: cefExtWeb,
	},
	{
		vendor: `Gravwell`, product: `authmonitor`, version: `1.7`,
		classID: `451`, name: `authentication failure`, severity: 7,
		ext: cefExtAuth,
	},
	{
		vendor: `Gravwell`, product: `endpoint|protect`, version: `4.0`, // pipe forces header escaping out in the wild
		classID: `909`, name: `malicious file quarantined`, severity: 9,
		ext: cefExtFile,
	},
}

func genDataCEF(ts time.Time) []byte {
	ec := cefEventClasses[rand.Intn(len(cefEventClasses))]
	sev := ec.severity - rand.Intn(cefMaxSeveritySkew)
	if sev < 0 {
		sev = 0
	}
	var sb strings.Builder
	// the CEF spec transport is syslog, so prepend a syslog style header
	fmt.Fprintf(&sb, "%s %s CEF:%d|%s|%s|%s|%s|%s|%d|",
		ts.Format(cefSyslogTsFormat), getHost(), cefVersion,
		cefHeaderReplacer.Replace(ec.vendor),
		cefHeaderReplacer.Replace(ec.product),
		cefHeaderReplacer.Replace(ec.version),
		cefHeaderReplacer.Replace(ec.classID),
		cefHeaderReplacer.Replace(ec.name), sev)
	sb.WriteString(ec.ext(ts))
	return []byte(sb.String())
}

var cefHeaderReplacer = strings.NewReplacer(
	`\`, `\\`,
	`|`, `\|`,
	"\n", ` `,
	"\r", ` `,
)

var cefExtReplacer = strings.NewReplacer(
	`\`, `\\`,
	`=`, `\=`,
	"\n", `\n`,
	"\r", `\r`,
)

// cefKV renders a single extension key=value pair with proper value escaping.
func cefKV(k, v string) string {
	return k + `=` + cefExtReplacer.Replace(v)
}

func cefJoin(kvs ...string) string {
	return strings.Join(kvs, ` `)
}

// cefTime renders a timestamp the way the CEF dictionary allows:
// milliseconds since epoch.
func cefTime(ts time.Time) string {
	return strconv.FormatInt(ts.UnixMilli(), 10)
}

func cefExtWorm(ts time.Time) string {
	src, dst := ips()
	spt, dpt := ports()
	return cefJoin(
		cefKV(`rt`, cefTime(ts)),
		cefKV(`src`, src),
		cefKV(`spt`, strconv.Itoa(spt)),
		cefKV(`dst`, dst),
		cefKV(`dpt`, strconv.Itoa(dpt)),
		cefKV(`proto`, `TCP`),
		cefKV(`act`, `blocked`),
		cefKV(`dvchost`, getHost()),
		cefKV(`msg`, `worm activity detected and stopped`),
	)
}

func cefExtFirewall(ts time.Time) string {
	src, dst := ips()
	spt, dpt := ports()
	proto := strings.ToUpper(getRandString(protos))
	return cefJoin(
		cefKV(`rt`, cefTime(ts)),
		cefKV(`src`, src),
		cefKV(`spt`, strconv.Itoa(spt)),
		cefKV(`dst`, dst),
		cefKV(`dpt`, strconv.Itoa(dpt)),
		cefKV(`proto`, proto),
		cefKV(`act`, `deny`),
		cefKV(`in`, strconv.Itoa(rand.Intn(0xffff))),
		cefKV(`out`, strconv.Itoa(rand.Intn(0xffff))),
		cefKV(`dvchost`, getHost()),
		cefKV(`reason`, `policy violation`),
	)
}

func cefExtWeb(ts time.Time) string {
	src, dst := ips()
	spt, _ := ports()
	user := getUser()
	methods := []string{`GET`, `GET`, `GET`, `POST`, `PUT`, `DELETE`, `HEAD`}
	return cefJoin(
		cefKV(`rt`, cefTime(ts)),
		cefKV(`src`, src),
		cefKV(`spt`, strconv.Itoa(spt)),
		cefKV(`dst`, dst),
		cefKV(`dpt`, `443`),
		cefKV(`suser`, user.User),
		cefKV(`app`, `HTTPS`),
		cefKV(`requestMethod`, getRandString(methods)),
		cefKV(`request`, `https://`+getDomain(0, 2)+`/`+fake.Lorem().Word()),
		cefKV(`requestClientApplication`, rd.UserAgentString()),
		cefKV(`out`, strconv.Itoa(rand.Intn(0xffffff))),
		cefKV(`in`, strconv.Itoa(rand.Intn(0xffff))),
	)
}

func cefExtAuth(ts time.Time) string {
	src, dst := ips()
	user := getUser()
	return cefJoin(
		cefKV(`rt`, cefTime(ts)),
		cefKV(`start`, cefTime(ts.Add(-time.Duration(rand.Intn(5000))*time.Millisecond))),
		cefKV(`end`, cefTime(ts)),
		cefKV(`src`, src),
		cefKV(`shost`, getHost()),
		cefKV(`dst`, dst),
		cefKV(`dhost`, getHost()),
		cefKV(`duser`, user.User),
		cefKV(`app`, `ssh`),
		cefKV(`outcome`, `failure`),
		cefKV(`reason`, `bad password`),
		cefKV(`msg`, fmt.Sprintf("failed login for user %s", user.User)),
	)
}

func cefExtFile(ts time.Time) string {
	user := getUser()
	// intentionally exercise extension escaping: paths contain backslashes
	path := `C:\Users\` + user.User + `\Downloads`
	fname := fake.Lorem().Word() + `.exe`
	return cefJoin(
		cefKV(`rt`, cefTime(ts)),
		cefKV(`suser`, user.User),
		cefKV(`shost`, getHost()),
		cefKV(`fname`, fname),
		cefKV(`filePath`, path+`\`+fname),
		cefKV(`fsize`, strconv.Itoa(rand.Intn(0x7fffff))),
		cefKV(`act`, `quarantine`),
		cefKV(`dvchost`, getHost()),
		cefKV(`msg`, `malicious file detected, hash=deadbeef`),
	)
}
