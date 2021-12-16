/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"math/rand"
	"strings"

	rd "github.com/Pallinder/go-randomdata"
)

const (
	maxGroups int = 64
	maxUsers  int = 1024 * 1024
)

var (
	groups []string
	users  []Account
)

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
