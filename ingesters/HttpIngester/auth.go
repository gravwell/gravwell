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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	cookieName       string = `_gravauth`
	jwtName          string = `_gravjwt`
	defaultTokenName string = `Bearer`

	_none    authType = ``
	none     authType = `none`
	basic    authType = `basic`
	jwtT     authType = `jwt`
	cookie   authType = `cookie`
	preToken authType = `preshared-token`
	preParam authType = `preshared-parameter`
	hdrToken authType = `preshared-header`

	userFormValue string = `username`
	passFormValue string = `password`
	issuer        string = `gravwell`
	authHeader    string = `Authorization`

	jwtDuration time.Duration = 24 * 2 * time.Hour
)

var (
	ErrInvalidAuthType    = errors.New("Invalid authentication type")
	ErrLoginURLRequired   = errors.New("Authentication type requires a login URL")
	ErrUnauthorized       = errors.New("Unauthorized")
	ErrMissingTokenName   = errors.New("Token name is invalid")
	ErrMissingTokenValue  = errors.New("Token value cannot be empty")
	ErrBadTokenValue      = errors.New("Bad token value")
	ErrMissingHeaderValue = errors.New("Token header value cannot be empty")
	ErrHeaderNotFound     = errors.New("Token header value not found")
)

type authType string

type auth struct {
	AuthType   authType
	Username   string
	Password   string `json:"-"` // DO NOT send this when marshalling
	LoginURL   string
	TokenName  string
	TokenValue string `json:"-"` // DO NOT send this when marshalling
}

type authHandler interface {
	Login(http.ResponseWriter, *http.Request)
	AuthRequest(*http.Request) error
}

func (a *auth) Validate() (enabled bool, err error) {
	//check the auth type and make sure a login url is set
	switch a.AuthType {
	case none: //do nothing
	case basic:
		//basic doesn't need a login url, just username and password
		if a.Username == `` {
			err = fmt.Errorf("Missing username for %s authentication", a.AuthType)
		} else if a.Password == `` {
			err = fmt.Errorf("Missing password for %s authentication", a.AuthType)
		} else {
			enabled = true
		}
	case jwtT:
		fallthrough
	case cookie:
		if a.LoginURL == `` {
			err = ErrLoginURLRequired
		} else if _, err = url.Parse(a.LoginURL); err != nil {
			err = fmt.Errorf("Invalid login url %s for %s authentication: %v", a.LoginURL, a.AuthType, err)
		} else if a.Username == `` {
			err = fmt.Errorf("Missing username for %s authentication", a.AuthType)
		} else if a.Password == `` {
			err = fmt.Errorf("Missing password for %s authentication", a.AuthType)
		} else {
			enabled = true
		}
	case preToken:
		fallthrough
	case preParam:
		if a.TokenName == `` {
			a.TokenName = defaultTokenName
		}
		if a.TokenValue == `` {
			err = fmt.Errorf("Missing Token-Value for auth type %s", a.AuthType)
			return
		}
		enabled = true
	case hdrToken:
		if a.TokenName == `` {
			a.TokenName = defaultTokenName
		}
		if a.TokenValue == `` {
			err = fmt.Errorf("Missing Token-Value for auth type %s", a.AuthType)
			return
		}
		enabled = true
	}
	return
}

func (a auth) NewAuthHandler(lgr *log.Logger) (url string, hnd authHandler, err error) {
	if lgr == nil {
		err = errors.New("Nil logger")
		return
	}
	switch a.AuthType {
	case _none:
		return
	case none:
		return
	case basic:
		hnd, err = newBasicAuthHandler(a.Username, a.Password, lgr)
	case jwtT:
		url = a.LoginURL
		hnd, err = newJWTAuthHandler(a.Username, a.Password, lgr)
	case cookie:
		url = a.LoginURL
		hnd, err = newCookieAuthHandler(a.Username, a.Password, lgr)
	case preToken:
		hnd, err = newPresharedTokenHandler(a.TokenName, a.TokenValue, lgr)
	case preParam:
		hnd, err = newPresharedParamHandler(a.TokenName, a.TokenValue, lgr)
	case hdrToken:
		hnd, err = newPresharedHeaderTokenHandler(a.TokenName, a.TokenValue, lgr)
	default:
		err = fmt.Errorf("Unknown authentication type %q", a.AuthType)
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
	case preToken:
	case preParam:
	case hdrToken:
	default:
		r = none
		err = ErrInvalidAuthType
	}
	return
}

type noLogin struct{}

func (n *noLogin) Login(w http.ResponseWriter, r *http.Request) {
	//this should never get there
	w.WriteHeader(http.StatusNotFound)
}

type basicAuthHandler struct {
	noLogin
	lgr  *log.Logger
	user string
	pass string
}

func newBasicAuthHandler(user, pass string, lgr *log.Logger) (hnd authHandler, err error) {
	hnd = &basicAuthHandler{
		lgr:  lgr,
		user: user,
		pass: pass,
	}
	return
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

type tokHandler struct {
	noLogin
	lgr      *log.Logger
	hdrName  string
	tokValue string
}

type preTokenHandler struct {
	tokHandler
}

func newPresharedHeaderTokenHandler(hdr, value string, lgr *log.Logger) (hnd authHandler, err error) {
	if value == `` {
		err = ErrMissingTokenValue
	} else if hdr == `` {
		err = ErrMissingHeaderValue
	} else {
		hnd = &preTokenHandler{
			tokHandler: tokHandler{
				lgr:      lgr,
				hdrName:  hdr,
				tokValue: value,
			},
		}
	}
	return
}

func newPresharedTokenHandler(name, value string, lgr *log.Logger) (hnd authHandler, err error) {
	return newPresharedHeaderTokenHandler(authHeader, name+" "+value, lgr)
}

func (pth *preTokenHandler) AuthRequest(r *http.Request) error {
	if tok, err := getHeaderToken(r, pth.hdrName); err != nil {
		return err
	} else if tok != pth.tokValue {
		return ErrUnauthorized
	}
	return nil
}

type preParamHandler struct {
	tokHandler
}

func newPresharedParamHandler(name, value string, lgr *log.Logger) (hnd authHandler, err error) {
	if name == `` {
		err = ErrMissingTokenName
	} else if value == `` {
		err = ErrMissingTokenValue
	} else {
		hnd = &preParamHandler{
			tokHandler: tokHandler{
				lgr:      lgr,
				hdrName:  name,
				tokValue: value,
			},
		}
	}
	return
}

func (pth *preParamHandler) AuthRequest(r *http.Request) error {
	tok, err := getParamToken(r, pth.hdrName)
	if err != nil {
		return err
	} else if tok != pth.tokValue {
		return ErrBadTokenValue
	}
	return nil
}

type jwtAuthHandler struct {
	lgr    *log.Logger
	secret string
	user   string
	pass   string
}

func randBase64(sz int) (s string, err error) {
	//generate a new random secret
	buff := make([]byte, sz)
	var n int
	if n, err = rand.Read(buff); err != nil {
		return
	} else if n != len(buff) {
		err = errors.New("Failed to generate random buffer")
		return
	}
	s = base64.StdEncoding.EncodeToString(buff)
	return
}

func newJWTAuthHandler(user, pass string, lgr *log.Logger) (hnd authHandler, err error) {
	//encode to base64
	var secret string
	if secret, err = randBase64(32); err == nil {
		hnd = &jwtAuthHandler{
			secret: secret,
			user:   user,
			pass:   pass,
			lgr:    lgr,
		}
	}
	return
}

func (jah *jwtAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var u, p string
	//parse the post form
	if err := r.ParseForm(); err != nil {
		jah.lgr.Info("bad login request", log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	//grab the
	u = r.FormValue(userFormValue)
	p = r.FormValue(passFormValue)
	if u != jah.user || p != jah.pass {
		w.WriteHeader(http.StatusForbidden)
		jah.lgr.Info("failed login", log.KV("address", getRemoteIP(r)))
		return
	}

	//user is good, generate the JWT
	now := time.Now().Unix()
	claims := &jwt.StandardClaims{
		NotBefore: now,
		ExpiresAt: now + int64(jwtDuration.Seconds()),
		Issuer:    issuer,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if ss, err := token.SignedString([]byte(jah.secret)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		jah.lgr.Info("bad JWT token", log.KV("address", getRemoteIP(r)), log.KVErr(err))
	} else {
		//set the header
		io.WriteString(w, ss)
		jah.lgr.Info("successful login", log.KV("address", getRemoteIP(r)))
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

type cookieAuthHandler struct {
	sync.Mutex
	lgr     *log.Logger
	user    string
	pass    string
	cookies map[string]time.Time
}

func newCookieAuthHandler(user, pass string, lgr *log.Logger) (hnd authHandler, err error) {
	if user == `` {
		err = errors.New("empty username")
	} else if pass == `` {
		err = errors.New("empty password")
	} else if lgr == nil {
		err = errors.New("empty logger")
	} else {
		hnd = &cookieAuthHandler{
			lgr:     lgr,
			user:    user,
			pass:    pass,
			cookies: make(map[string]time.Time),
		}
	}
	return
}

func (cah *cookieAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var u, p string
	//parse the post form
	if err := r.ParseForm(); err != nil {
		cah.lgr.Info("bad login request", log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	//grab the
	u = r.FormValue(userFormValue)
	p = r.FormValue(passFormValue)
	if u != cah.user || p != cah.pass {
		w.WriteHeader(http.StatusForbidden)
		cah.lgr.Info("failed login", log.KV("address", getRemoteIP(r)))
		return
	}
	expires := time.Now().UTC().Add(jwtDuration)
	//make a cookie
	cookie, err := randBase64(32)
	if err != nil {
		cah.lgr.Error("failed to generate cookie", log.KVErr(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	cah.Lock()
	//add this cookie
	cah.cookies[cookie] = expires
	now := time.Now()
	for k, v := range cah.cookies {
		//expire cookies
		if now.After(v) {
			delete(cah.cookies, k)
		}
	}
	cah.Unlock()
	c := http.Cookie{
		Name:    cookieName,
		Value:   cookie,
		Expires: expires,
		Path:    `/`,
	}
	http.SetCookie(w, &c)
	return
}

func (cah *cookieAuthHandler) AuthRequest(r *http.Request) (err error) {
	var c *http.Cookie
	if c, err = r.Cookie(cookieName); err != nil {
		return
	}
	if c == nil || c.Value == `` {
		err = errors.New("invalid cookie")
		return
	}
	n := time.Now()
	cah.Lock()
	expires, ok := cah.cookies[c.Value]
	if ok {
		if n.After(expires) {
			delete(cah.cookies, c.Value)
			err = errors.New("Session expired")
		}
	} else {
		err = errors.New("Unauthorized")
	}
	cah.Unlock()
	return
}

func getJWTToken(r *http.Request) (string, error) {
	return getAuthToken(r, `Bearer`)
}

func getHeaderToken(r *http.Request, hdrName string) (ret string, err error) {
	if hdrName == `` {
		err = errors.New("Empty header name")
		return
	} else if len(r.Header) == 0 {
		err = ErrHeaderNotFound
	} else if vals, ok := r.Header[hdrName]; !ok || len(vals) == 0 {
		err = ErrHeaderNotFound
	} else {
		ret = vals[0]
	}
	return
}

func getAuthToken(r *http.Request, tokName string) (ret string, err error) {
	var hv string
	if hv, err = getHeaderToken(r, `Authorization`); err != nil {
		return
	}
	bits := strings.Fields(hv)
	if len(tokName) == 0 && len(bits) == 1 {
		ret = bits[0]
	} else if len(tokName) > 0 && len(bits) == 2 && bits[0] == tokName {
		ret = bits[1]
	} else {
		err = errors.New("invalid auth token")
	}
	return
}

func getParamToken(r *http.Request, tokName string) (ret string, err error) {
	if tokName == `` {
		err = errors.New("Empty token name")
		return
	}
	keys, ok := r.URL.Query()[tokName]
	if !ok || len(keys) == 0 {
		err = fmt.Errorf("Missing %s parameter", tokName)
		return
	}
	//be lenient and just get the first non-empty key
	for _, k := range keys {
		if len(k) != 0 {
			ret = k
			break
		}
	}
	return
}
