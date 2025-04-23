/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/generators/ipgen"
	"github.com/jaswdr/faker/v2"
)

const (
	maxGroups int = 64
	maxUsers  int = 256 * 1024
	maxHosts  int = 512 * 1024
	maxApps   int = 25000
	minCount  int = 10 //min randomness for seeding

	tsFormat string = `2006-01-02T15:04:05.999999Z07:00`
)

// this is all Zeek stuff:
var (
	protos   = []string{"icmp", "tcp", "udp"}
	alphabet = []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	services = map[string][]string{
		"icmp": []string{"-"},
		"tcp":  []string{"-", "http", "ssl", "ssh"},
		"udp":  []string{"-", "dns", "dhcp", "krb", "dtls"},
	}
	operating_systems = []string{
		`windows`,
		`linux`,
		`macos`,
		`sun solaris`,
		`beos`,
		`android`,
	}
	states = []string{
		"OTH",
		"SF",
		"S0",
		"SH",
		"SHR",
		"RSTR",
		"S1",
		"RSTO",
		"RSTRH",
		"S3",
	}

	histories = []string{
		"Dc",
		"ShADadFf",
		"-",
		"Dd",
		"C",
		"D",
		"HcADF",
		"ShADadfF",
		"CC",
		"S",
		"ShADadFRfR",
		"DadA",
		"^dDa",
		"ShADad",
		"ShADadfFr",
		"ShADadR",
		"HcAD",
		"ShADadFfR",
		"Cd",
		"^d",
		"ShADdaFf",
		"DFdfR",
		"ShR",
		"^dADa",
		"Sh",
		"ShADadfR",
		"^dDaA",
		"ShADadtFf",
		"ShADadf",
		"ShADadfFR",
		"ShAFf",
		"ShADadFfrrr",
		"DFafA",
		"ShADadtfFrr",
		"ShADadFRf",
		"ShADadr",
		"^dfADFr",
		"ShADdaFfR",
		"DFdfrR",
		"ShADdFaf",
		"ShADadtFfR",
		"ShADadfr",
		"ShADadttttFf",
		"ShADadFfrr",
		"ShADadtttFf",
		"ShADaFdRfR",
		"ShADadFfT",
		"ShADadFfRRR",
		"ShADFadRfR",
		"ShADadtfRrr",
		"ShADadtfF",
		"ShADFadfR",
		"DFr",
		"DadAt",
		"DFdrrR",
		"DadAf",
		"^dfA",
		"ShADFadRf",
		"Fr",
		"ShADadtR",
		"ShADadFfRR",
		"ShADdfFa",
		"ShADadtfFR",
		"ShADadftR",
		"DFdrR",
		"DFadfR",
		"ShADadttf",
		"SW",
		"ShADadTtfFrr",
		"^r",
		"ShADadFTfR",
	}
)

var (
	groups        []string
	users         []Account
	complexGroups []ComplexGroup

	hosts []string
	apps  []string

	v4gen      *ipgen.V4Gen
	v6gen      *ipgen.V6Gen
	serverIPs  []net.IP
	serverIP6s []net.IP

	fake = faker.New()
)

func init() {
	var err error

	v4gen, err = ipgen.RandomWeightedV4Generator(40)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v", err)
	}
	v6gen, err = ipgen.RandomWeightedV6Generator(30)
	if err != nil {
		log.Fatalf("Failed to instantiate v6 generator: %v\n", err)
	}
	for i := 0; i < 4; i++ {
		serverIPs = append(serverIPs, v4gen.IP())
	}
	for i := 0; i < 4; i++ {
		serverIP6s = append(serverIP6s, v6gen.IP())
	}
}

type Account struct {
	User    string `json:"user" xml:"user,attr"`
	Name    string `json:"name" xml:"name,attr"`
	Email   string `json:"email" xml:"email,attr"`
	Phone   string `json:"phone" xml:"phone,attr"`
	Address string `json:"address"`
	State   string `json:"state"`
	Country string `json:"country"`
}

type ComplexGroup struct {
	ID         int               `json:"gid" xml:"gid,attr"`
	Name       string            `json:"name" xml:"name,attr"`
	Division   string            `json:"division" xml:"division,attr"`
	Attributes []string          `json:"attributes,omitempty" xml:"attributes,attr"`
	Locations  []ComplexLocation `json:"location" xml:"location,attr"`
}

type ComplexLocation struct {
	Lat     float64 `json:"lat" xml:"lat,attr"`
	Long    float64 `json:"long" xml:"long,attr"`
	Country string  `json:"country" xml:"country,attr"`
	State   string  `json:"state" xml:"state,attr"`
}

func seedVars(cnt int) {
	seedUsers(overrideCount(cnt, minCount, maxUsers, `USER_COUNT`), overrideCount(cnt, minCount, maxGroups, `GROUP_COUNT`))
	seedHosts(overrideCount(int(float64(cnt)/50.0), minCount, maxHosts, `HOST_COUNT`))
	seedApps(overrideCount(int(float64(cnt)*50.0), minCount, maxApps, `APP_COUNT`))
}

func seedUsers(usercount, gcount int) {
	if usercount > maxUsers {
		usercount = maxUsers
	}
	if gcount > maxGroups {
		gcount = maxGroups
	}
	for i := 0; i < gcount; i++ {
		name := rd.Noun()
		groups = append(groups, name)
		complexGroups = append(complexGroups, ComplexGroup{
			ID:         i,
			Name:       name,
			Division:   rd.FirstName(rd.RandomGender),
			Attributes: randAttributes(4),
			Locations:  randLocations(2),
		})
	}

	for i := 0; i < usercount; i++ {
		email := rd.Email()
		user := strings.Split(email, "@")[0]
		a := Account{
			User:    user,
			Name:    rd.FullName(i & 1),
			Email:   email,
			Phone:   rd.PhoneNumber(),
			Address: rd.Address(),
			State:   rd.State(rd.Small),
			Country: rd.Country(rd.FullCountry),
		}
		users = append(users, a)
	}
}

func seedHosts(cnt int) {
	fint := fake.Internet()
	hosts = make([]string, 0, cnt)
	for i := 0; i < cnt; i++ {
		if (i & 1) == 0 {
			hosts = append(hosts, rd.Noun())
		} else {
			hosts = append(hosts, fint.Domain())
		}
	}
}

func seedApps(cnt int) {
	for i := 0; i < cnt; i++ {
		apps = append(apps, fake.App().Name())
	}
}

func getRandString(v []string) (r string) {
	if len(v) > 0 {
		r = v[rand.Intn(len(v))]
	}
	return
}

func getOS() string {
	return getRandString(operating_systems)
}

func getUser() Account {
	return users[rand.Intn(len(users))]
}

func getGroup() string {
	return groups[rand.Intn(len(groups))]
}

func getComplexGroup() ComplexGroup {
	return complexGroups[rand.Intn(len(complexGroups))]
}

func getGroups() (r []string) {
	if cnt := rand.Intn(3); cnt > 0 {
		r = make([]string, cnt)
		for i := range r {
			r[i] = getGroup()
		}
	}
	return
}

func getComplexGroups() (r []ComplexGroup) {
	if cnt := rand.Intn(3); cnt > 0 {
		r = make([]ComplexGroup, cnt)
		for i := range r {
			r[i] = getComplexGroup()
		}
	}
	return
}

func getHost() string {
	return hosts[rand.Intn(len(hosts))]
}

func getApp() string {
	return apps[rand.Intn(len(apps))]
}

func getIP() net.IP {
	if r := rand.Int(); r&0x3ff == 0x3ff {
		return nil // 1/1024 IPs is not populated
	} else if r&0x3 == 0x3 {
		//25% are IPv6
		return v6gen.IP()
	}
	return v4gen.IP() //everything else is IPv4
}

func ips() (string, string) {
	if (rand.Int() & 3) == 0 {
		//more IPv4 than 6
		return v6gen.IP().String(), v6gen.IP().String()
	}
	return v4gen.IP().String(), v4gen.IP().String()
}

func ports() (int, int) {
	var orig_port, resp_port int
	if rand.Int()%2 == 0 {
		orig_port = 1 + rand.Intn(2048)
		resp_port = 2048 + rand.Intn(0xffff-2048)
	} else {
		orig_port = 2048 + rand.Intn(0xffff-2048)
		resp_port = 1 + rand.Intn(2048)
	}
	return orig_port, resp_port
}

func randomBase62(l int) string {
	r := make([]byte, l)
	for i := 0; i < l; i++ {
		r[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(r)
}

func randAttributes(max int) (r []string) {
	n := rand.Intn(max) + 1
	p := strings.Fields(rd.Paragraph())
	if len(p) < n {
		r = p
	} else {
		r = p[0:n]
	}
	return
}

func randLocations(max int) (r []ComplexLocation) {
	n := rand.Intn(max) + 1
	r = make([]ComplexLocation, 0, n)
	for i := 0; i < n; i++ {
		r = append(r, ComplexLocation{
			Country: rd.Country(rd.FullCountry),
			State:   rd.Locale(),
			Lat:     randLatLong(),
			Long:    randLatLong(),
		})
	}
	return
}

func randLatLong() float64 {
	v := rand.Float64()
	negative := v < 0
	if negative {
		v = v * -1
	}
	v = math.Mod(v, 180.0)
	if negative {
		v = v * -1
	}
	return roundFloat(v, 4)
}

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return float64(math.Round(val*ratio) / ratio)
}

func overrideCount(cnt, min, max int, envName string) int {
	if cnt > max {
		cnt = max
	} else if cnt < min {
		cnt = min
	}
	val := os.Getenv(envName)
	if envName == `` || val == `` {
		return cnt
	}
	if v, err := strconv.ParseInt(val, 10, 64); err == nil || v > 0 {
		cnt = int(v)
	}
	return cnt
}
