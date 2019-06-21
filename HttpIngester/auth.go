/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

const (
	cookieName string = `_gravauth`
	jwtName    string = `_gravjwt`

	_none  authType = ``
	none   authType = `none`
	basic  authType = `basic`
	jwtT   authType = `jwt`
	cookie authType = `cookie`

	userFormValue string = `username`
	passFormValue string = `password`
	issuer        string = `gravwell`

	jwtDuration int64 = 3600 * 24 * 2
)

var (
	ErrInvalidAuthType  = errors.New("Invalid authentication type")
	ErrLoginURLRequired = errors.New("Authentication type requires a login URL")
)

type authType string

type auth struct {
	AuthType authType
	Username string
	Password string
	LoginURL string
}

type authHandler interface {
	Login(http.ResponseWriter, *http.Request)
	AuthRequest(*http.Request) error
}

func (a auth) Validate() (enabled bool, err error) {
	enabled = a.AuthType != none && a.Username != `` && a.Password != ``
	if enabled {
		//check the auth type and make sure a login url is set
		switch a.AuthType {
		case basic: //basic doesn't need a login url
		case jwtT:
			fallthrough
		case cookie:
			if a.LoginURL == `` {
				err = ErrLoginURLRequired
				enabled = false
			} else if _, err = url.Parse(a.LoginURL); err != nil {
				enabled = false
			}
		}
	}
	return
}

func (a auth) NewAuthHandler() (url string, hnd authHandler, err error) {
	switch a.AuthType {
	case _none:
		return
	case none:
		return
	case basic:
		hnd, err = newBasicAuthHandler(a.Username, a.Password)
	case jwtT:
		url = a.LoginURL
		hnd, err = newJWTAuthHandler(a.Username, a.Password)
	case cookie:
		url = a.LoginURL
		hnd, err = newCookieAuthHandler(a.Username, a.Password)
	default:
		err = errors.New("Unknown authentication type")
	}
	return
}

func parseAuthType(v string) (r authType, err error) {
	r = authType(strings.TrimSpace(strings.ToLower(v)))
	switch r {
	case _none:
		r = none
	case none:
	case basic:
	case jwtT:
	case cookie:
	default:
		r = none
		err = ErrInvalidAuthType
	}
	return
}

type basicAuthHandler struct {
	user string
	pass string
}

func newBasicAuthHandler(user, pass string) (hnd authHandler, err error) {
	hnd = &basicAuthHandler{
		user: user,
		pass: pass,
	}
	return
}

func (bah *basicAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	//this should never get there
	w.WriteHeader(http.StatusNotFound)
}

func (bah *basicAuthHandler) AuthRequest(r *http.Request) error {
	var u, p string
	var ok bool
	//try to grab the basic auth header
	if u, p, ok = r.BasicAuth(); !ok {
		return errors.New("Missing authentication")
	}
	if !((u == bah.user) && (p == bah.pass)) {
		return errors.New("Bad username or password")
	}
	return nil
}

type jwtAuthHandler struct {
	secret string
	user   string
	pass   string
}

func newJWTAuthHandler(user, pass string) (hnd authHandler, err error) {
	//generate a new random secret
	buff := make([]byte, 32)
	var n int
	if n, err = rand.Read(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to generate random buffer")
		return
	}
	//encode to base64
	return &jwtAuthHandler{
		secret: base64.StdEncoding.EncodeToString(buff),
		user:   user,
		pass:   pass,
	}, nil
}

func (jah *jwtAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var u, p string
	//parse the post form
	if err := r.ParseForm(); err != nil {
		lg.Info("bad login request %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	//grab the
	u = r.FormValue(userFormValue)
	p = r.FormValue(passFormValue)
	if u != jah.user || p != jah.pass {
		w.WriteHeader(http.StatusForbidden)
		lg.Info("%v Failed login", getRemoteIP(r))
		return
	}

	//user is good, generate the JWT
	now := time.Now().Unix()
	claims := &jwt.StandardClaims{
		NotBefore: now,
		ExpiresAt: now + jwtDuration,
		Issuer:    issuer,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if ss, err := token.SignedString([]byte(jah.secret)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		lg.Info("%v Bad JWT token: %v", getRemoteIP(r), err)
	} else {
		//set the header
		io.WriteString(w, ss)
		lg.Info("%v Successful login", getRemoteIP(r))
	}
	return
}

func (bah *jwtAuthHandler) AuthRequest(r *http.Request) error {
	ss, err := getJWTToken(r)
	if err != nil {
		return err
	}
	var claims jwt.StandardClaims
	//attempt to validate the signed string
	tok, err := jwt.ParseWithClaims(ss, &claims, bah.secretParser)
	if err != nil {
		return err
	}
	t := time.Now().Unix()
	if !tok.Valid {
		return errors.New("invalid token")
	} else if err := tok.Claims.Valid(); err != nil {
		return err
	} else if err := claims.Valid(); err != nil {
		return err
	} else {
		//claims were able to be cast, check expirations and issuer
		if claims.Issuer != issuer || t < claims.NotBefore || t > claims.ExpiresAt {
			return errors.New("token expired")
		}
	}
	return nil
}

func (bah *jwtAuthHandler) secretParser(token *jwt.Token) (interface{}, error) {
	// Don't forget to validate the alg is what you expect:
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, errors.New("Unexpected signing method")
	}
	return []byte(bah.secret), nil
}

func newCookieAuthHandler(user, pass string) (hnd authHandler, err error) {
	err = errors.New("Not ready")
	return
}

func getJWTToken(r *http.Request) (ret string, err error) {
	for k, v := range r.Header {
		if k == `Authorization` {
			for _, vv := range v {
				if strings.HasPrefix(vv, "Bearer ") {
					ret = strings.TrimPrefix(vv, "Bearer ")
					return
				}
			}
		}
	}
	err = errors.New("No JWT token")
	return
}
