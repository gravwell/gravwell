/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"log"
	"math/rand"
	"net"
	"strings"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
)

const (
	maxGroups int = 64
	maxUsers  int = 1024 * 1024

	hcount   int    = 32
	appcount int    = 2048
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
	groups []string
	users  []Account

	hosts []string
	apps  []string

	v4gen      *ipgen.V4Gen
	v6gen      *ipgen.V6Gen
	serverIPs  []net.IP
	serverIP6s []net.IP
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

	for i := 0; i < hcount; i++ {
		hosts = append(hosts, rd.Noun())
	}
	for i := 0; i < appcount; i++ {
		apps = append(apps, rd.Adjective())
	}
}

type Account struct {
	User    string `json:"user"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	State   string `json:"state"`
	Country string `json:"country"`
}

func seedUsers(usercount, gcount int) {
	if usercount > maxUsers {
		usercount = maxUsers
	}
	if gcount > maxGroups {
		gcount = maxGroups
	}
	for i := 0; i < gcount; i++ {
		groups = append(groups, rd.Noun())
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

func getUser() Account {
	return users[rand.Intn(len(users))]
}

func getGroup() string {
	return groups[rand.Intn(len(groups))]
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

func getHost() string {
	return hosts[rand.Intn(len(hosts))]
}

func getApp() string {
	return apps[rand.Intn(len(apps))]
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
