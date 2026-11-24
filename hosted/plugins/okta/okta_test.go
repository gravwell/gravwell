/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/okta/okta-sdk-golang/v6/okta"
	"golang.org/x/time/rate"
)

const (
	testHost     = `example.okta.com`
	testBaseURL  = `http://` + testHost // the fake serves plain http, every connection is dialed to it
	testClientID = `0oatestclient`
	testSSWS     = `ssws-test-token`
	bothScopes   = `okta.logs.read okta.users.read`
)

// fakeOkta implements the token endpoint, including the DPoP handshake, and the system log and
// users APIs. Pagination cursors are plain indexes into the logs and users slices.
type fakeOkta struct {
	ln        *pipeListener
	pub       *rsa.PublicKey
	kid       string // expected client assertion kid
	dpop      bool   // require DPoP at the token endpoint
	scope     string // granted scope
	userPage  int    // users per page
	nonce     string
	reject401 int  // API requests to answer with 401, revoking every token
	reject429 int  // API requests to answer with 429
	skip429   int  // API requests to let through before reject429 applies
	token503  int  // token requests to answer with 503
	token429  int  // token requests to answer with 429, rate limited for about a second
	nonceLoop bool // answer every DPoP proof with use_dpop_nonce
	noNonce   bool // leave the DPoP-Nonce header off the use_dpop_nonce reply
	// name use_dpop_nonce only in the error description, the SDK still treats that as a nonce request
	nonceInDesc bool
	foreignNext bool // hand out next links on another host

	mu         sync.Mutex
	logs       []string
	users      []string
	tokens     map[string]string // access token -> bound DPoP key, empty for Bearer
	scopes     map[string]string // access token -> scope granted when it was issued
	issued     []string
	jtis       map[string]bool
	tokenErrs  []string // token endpoint OAuth errors, in order
	logQueries []url.Values
	userQuery  []url.Values
	schemes    []string // API Authorization schemes, in order
	failures   []string // proof or assertion validation failures
}

// newFakeOkta starts the fake, opts configure it before it serves anything
func newFakeOkta(t *testing.T, opts ...func(*fakeOkta)) *fakeOkta {
	f := &fakeOkta{
		pub:      &testKey().PublicKey,
		scope:    bothScopes,
		userPage: 2,
		nonce:    `nonce-` + randHex(8),
		tokens:   map[string]string{},
		scopes:   map[string]string{},
		jtis:     map[string]bool{},
	}
	for _, opt := range opts {
		opt(f)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(`POST /oauth2/v1/token`, f.token)
	mux.HandleFunc(`GET /api/v1/logs`, f.api(`okta.logs.read`, f.systemLogs))
	mux.HandleFunc(`GET /api/v1/users`, f.api(`okta.users.read`, f.listUsers))
	f.ln = newPipeListener()
	srv := &httptest.Server{Listener: f.ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, fl := range f.failures {
			t.Errorf("fake okta rejected a request: %s", fl)
		}
	})
	return f
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// transport dials every connection to the fake, whatever the host. The SDK drops the port from
// the org URL, so pointing the base URL at a listener would not be enough anyway.
func (f *fakeOkta) transport(t *testing.T) http.RoundTripper {
	tr := &http.Transport{DialContext: f.ln.dial}
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}

// pipeListener is an in-process net.Listener, the fake speaks real HTTP without binding a port
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return `pipe` }
func (pipeAddr) String() string  { return `pipe` }

func (f *fakeOkta) fail(format string, args ...any) {
	f.failures = append(f.failures, fmt.Sprintf(format, args...))
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set(`Content-Type`, `application/json`)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{`error`: code, `error_description`: desc})
}

func (f *fakeOkta) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tokenURL := `http://` + r.Host + tokenPath
	if f.token429 > 0 {
		f.token429--
		f.tokenErrs = append(f.tokenErrs, `429`)
		w.Header().Set(`X-Rate-Limit-Reset`, strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		oauthError(w, http.StatusTooManyRequests, `too_many_requests`, `API call exceeded rate limit due to too many requests.`)
		return
	}
	if f.token503 > 0 {
		f.token503--
		f.tokenErrs = append(f.tokenErrs, `503`)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		f.fail("bad token form: %v", err)
		oauthError(w, 400, `invalid_request`, `bad form`)
		return
	}
	if r.PostForm.Get(`grant_type`) != `client_credentials` ||
		r.PostForm.Get(`client_assertion_type`) != `urn:ietf:params:oauth:client-assertion-type:jwt-bearer` {
		f.fail("bad grant: %v", r.PostForm)
		oauthError(w, 400, `invalid_request`, `bad grant`)
		return
	}
	if !slices.Equal(strings.Fields(r.PostForm.Get(`scope`)), strings.Fields(bothScopes)) {
		f.fail("unexpected scope request %q", r.PostForm.Get(`scope`))
	}

	assertion, err := jwt.Parse(r.PostForm.Get(`client_assertion`), func(*jwt.Token) (any, error) { return f.pub, nil },
		jwt.WithValidMethods([]string{`RS256`}), jwt.WithIssuer(testClientID), jwt.WithSubject(testClientID),
		jwt.WithAudience(tokenURL), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		f.tokenErrs = append(f.tokenErrs, `invalid_client`)
		oauthError(w, 401, `invalid_client`, `The client_assertion signature is invalid.`)
		return
	}
	if kid, _ := assertion.Header[`kid`].(string); kid != f.kid {
		f.fail("client assertion kid %q, want %q", kid, f.kid)
	}
	if jti, _ := assertion.Claims.(jwt.MapClaims)[`jti`].(string); jti == `` || f.jtis[jti] {
		f.fail("client assertion jti missing or replayed")
	} else {
		f.jtis[jti] = true
	}

	tokenType, binding := `Bearer`, ``
	if f.dpop {
		proof := r.Header.Get(`DPoP`)
		if proof == `` {
			f.tokenErrs = append(f.tokenErrs, `invalid_dpop_proof`)
			oauthError(w, 400, `invalid_dpop_proof`, `The DPoP proof JWT header is missing.`)
			return
		}
		claims, key, err := f.verifyProof(proof, http.MethodPost, tokenURL, ``)
		if err != nil {
			f.fail("token DPoP proof: %v", err)
			oauthError(w, 400, `invalid_dpop_proof`, err.Error())
			return
		}
		if f.nonceLoop || claims[`nonce`] != f.nonce {
			f.tokenErrs = append(f.tokenErrs, `use_dpop_nonce`)
			if !f.noNonce {
				w.Header().Set(`DPoP-Nonce`, f.nonce)
			}
			if f.nonceInDesc {
				oauthError(w, 400, `invalid_dpop_proof`, `use_dpop_nonce: a nonce is required in the DPoP proof.`)
				return
			}
			oauthError(w, 400, `use_dpop_nonce`, `Authorization server requires nonce in DPoP proof.`)
			return
		}
		tokenType, binding = `DPoP`, key
	}
	at := `at-` + randHex(16)
	f.tokens[at] = binding
	f.scopes[at] = f.scope
	f.issued = append(f.issued, at)
	w.Header().Set(`Content-Type`, `application/json`)
	json.NewEncoder(w).Encode(map[string]any{
		`token_type`: tokenType, `expires_in`: 3600, `access_token`: at, `scope`: f.scope,
	})
}

// verifyProof checks a DPoP proof JWT and returns its claims and the modulus of its key
func (f *fakeOkta) verifyProof(proof, htm, htu, accessToken string) (jwt.MapClaims, string, error) {
	var keyID string
	tok, err := jwt.Parse(proof, func(tok *jwt.Token) (any, error) {
		if typ, _ := tok.Header[`typ`].(string); typ != `dpop+jwt` {
			return nil, fmt.Errorf("typ %q", typ)
		}
		jwk, _ := tok.Header[`jwk`].(map[string]any)
		n, _ := jwk[`n`].(string)
		e, _ := jwk[`e`].(string)
		if _, private := jwk[`d`]; private || n == `` || e == `` {
			return nil, errors.New("jwk header is not an RSA public key")
		}
		nb, err := base64.RawURLEncoding.DecodeString(n)
		if err != nil {
			return nil, err
		}
		eb, err := base64.RawURLEncoding.DecodeString(e)
		if err != nil {
			return nil, err
		}
		keyID = n
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}, nil
	}, jwt.WithValidMethods([]string{`RS256`}), jwt.WithIssuedAt())
	if err != nil {
		return nil, ``, err
	}
	claims := tok.Claims.(jwt.MapClaims)
	if claims[`htm`] != htm || claims[`htu`] != htu {
		return nil, ``, fmt.Errorf("proof bound to %v %v, want %s %s", claims[`htm`], claims[`htu`], htm, htu)
	}
	jti, _ := claims[`jti`].(string)
	if jti == `` || f.jtis[jti] {
		return nil, ``, errors.New("proof jti missing or replayed")
	}
	f.jtis[jti] = true
	if accessToken != `` {
		sum := sha256.Sum256([]byte(accessToken))
		if claims[`ath`] != base64.RawURLEncoding.EncodeToString(sum[:]) {
			return nil, ``, errors.New("proof ath does not match the access token")
		}
	}
	return claims, keyID, nil
}

// api authorizes a request against the SSWS token or an issued access token and the scope it needs
func (f *fakeOkta) api(scope string, next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		scheme, tok, _ := strings.Cut(r.Header.Get(`Authorization`), ` `)
		f.schemes = append(f.schemes, scheme)
		var ok bool
		switch scheme {
		case `SSWS`:
			ok = tok == testSSWS
		case `Bearer`:
			binding, found := f.tokens[tok]
			ok = found && binding == ``
		case `DPoP`:
			if binding, found := f.tokens[tok]; found && binding != `` {
				claims, key, err := f.verifyProof(r.Header.Get(`DPoP`), r.Method, `http://`+r.Host+r.URL.Path, tok)
				if err != nil {
					f.fail("API DPoP proof: %v", err)
				}
				ok = err == nil && key == binding && claims[`nonce`] == f.nonce
			}
		}
		if ok && scheme != `SSWS` && f.reject401 > 0 {
			f.reject401--
			clear(f.tokens)
			ok = false
		}
		if !ok {
			w.Header().Set(`Content-Type`, `application/json`)
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"errorCode":"E0000011","errorSummary":"Invalid token provided","errorLink":"E0000011","errorId":"oae1","errorCauses":[]}`)
			return
		}
		if f.skip429 > 0 {
			f.skip429--
		} else if f.reject429 > 0 {
			f.reject429--
			w.Header().Set(`X-Rate-Limit-Reset`, strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// a token keeps the scopes it was issued with, a later grant needs a new token
		if scheme != `SSWS` && !slices.Contains(strings.Fields(f.scopes[tok]), scope) {
			// okta answers a missing scope with an empty body, the reason is only in WWW-Authenticate
			w.Header().Set(`WWW-Authenticate`, fmt.Sprintf(`%s algs="RS256", error="insufficient_scope", error_description="The access token provided does not contain the required scopes.", resource="%s"`, scheme, r.URL.Path))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func writePage(w http.ResponseWriter, page []string, links ...string) {
	for _, l := range links {
		w.Header().Add(`Link`, l)
	}
	w.Header().Set(`Content-Type`, `application/json`)
	io.WriteString(w, "[\n  "+strings.Join(page, ",\n  ")+"\n]")
}

// systemLogs always hands out a next link, an exhausted log is an empty page
func (f *fakeOkta) systemLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.logQueries = append(f.logQueries, q)
	limit, err := strconv.Atoi(q.Get(`limit`))
	if err != nil || limit <= 0 {
		limit = 100
	}
	start, _ := strconv.Atoi(q.Get(`after`))
	start = min(start, len(f.logs))
	end := min(start+limit, len(f.logs))
	next := url.Values{`after`: {strconv.Itoa(end)}, `limit`: {q.Get(`limit`)}, `since`: {q.Get(`since`)}, `sortOrder`: {q.Get(`sortOrder`)}}
	writePage(w, f.logs[start:end],
		fmt.Sprintf(`<http://%s%s?%s>; rel="self"`, r.Host, r.URL.Path, r.URL.RawQuery),
		fmt.Sprintf(`<http://%s%s?%s>; rel="next"`, f.nextHost(r), r.URL.Path, next.Encode()))
}

// listUsers hands out a next link until the last page
func (f *fakeOkta) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.userQuery = append(f.userQuery, q)
	start, _ := strconv.Atoi(q.Get(`after`))
	start = min(start, len(f.users))
	end := min(start+f.userPage, len(f.users))
	links := []string{fmt.Sprintf(`<http://%s%s?%s>; rel="self"`, r.Host, r.URL.Path, r.URL.RawQuery)}
	if end < len(f.users) {
		next := url.Values{`after`: {strconv.Itoa(end)}, `limit`: {q.Get(`limit`)}, `search`: {q.Get(`search`)}}
		links = append(links, fmt.Sprintf(`<http://%s%s?%s>; rel="next"`, f.nextHost(r), r.URL.Path, next.Encode()))
	}
	writePage(w, f.users[start:end], links...)
}

func (f *fakeOkta) snapshot() (logQueries, userQueries []url.Values, schemes, issued, tokenErrs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.logQueries), slices.Clone(f.userQuery), slices.Clone(f.schemes), slices.Clone(f.issued), slices.Clone(f.tokenErrs)
}

var eventBase = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// testEvents are system log events, deliberately not compact so byte-identical ingest is visible
func testEvents(n int) (out []string) {
	for i := range n {
		out = append(out, fmt.Sprintf(`{"uuid": "evt-%d",  "published":"%s", "eventType":"user.session.start", "actor":{ "id":"00u%d", "type":"User" }}`,
			i, eventBase.Add(time.Duration(i)*time.Second).Format(timeFormat), i))
	}
	return
}

func testUsers(n int) (out []string) {
	for i := range n {
		out = append(out, fmt.Sprintf(`{"id": "00u%d", "status":"ACTIVE",  "lastUpdated":"%s", "profile":{"login":"user%d@example.com"}}`,
			i, eventBase.Format(timeFormat), i))
	}
	return
}

func newSSWSIngester(t *testing.T, f *fakeOkta, rt *testRuntime) *OktaIngester {
	t.Helper()
	o, err := NewOktaIngester(Config{Domain: testHost, Token: testSSWS, Request_Batch_Size: 2}, rt)
	if err != nil {
		t.Fatal(err)
	}
	o.baseURL = testBaseURL
	o.c = sswsDoer{next: &http.Client{Transport: f.transport(t), Timeout: 10 * time.Second}, token: testSSWS}
	o.latestTS = eventBase.Add(-time.Hour)
	return o
}

// newOAuthIngester builds an OAuth ingester against the fake, init also builds the doer as Run does
func newOAuthIngester(t *testing.T, f *fakeOkta, rt *testRuntime, init bool, mut func(*Config)) *OktaIngester {
	t.Helper()
	cfg := Config{Domain: testHost, Client_ID: testClientID, Private_Key: pkcs8PEM(t, testKey()), Request_Batch_Size: 2}
	if mut != nil {
		mut(&cfg)
	}
	o, err := NewOktaIngester(cfg, rt)
	if err != nil {
		t.Fatal(err)
	}
	o.baseURL = testBaseURL
	o.oauthBase = f.transport(t)
	o.latestTS = eventBase.Add(-time.Hour)
	if init {
		d := o.newOAuthDoer(t.Context(), rate.NewLimiter(rate.Inf, 1), rt)
		d.backoff = time.Millisecond
		o.c = d
	}
	return o
}

func assertData(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("ingested data mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func assertSchemes(t *testing.T, got []string, want string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("no API requests")
	}
	for _, s := range got {
		if s != want {
			t.Fatalf("API authorization schemes %v, want all %s", got, want)
		}
	}
}

// assertClean fails on error logs and on any token or key material in a log line
func assertClean(t *testing.T, rt *testRuntime, f *fakeOkta, allowErrors bool) {
	t.Helper()
	_, _, _, issued, _ := f.snapshot()
	for _, l := range rt.logged(``) {
		if !allowErrors && (l.level == `ERROR` || l.level == `CRITICAL`) {
			t.Errorf("unexpected log: %s", l)
		}
		s := l.String()
		for _, tok := range issued {
			if strings.Contains(s, tok) {
				t.Fatalf("access token logged: %s", l.msg)
			}
		}
		assertNoFragments(t, s, pemBody(pkcs8PEM(t, testKey())))
	}
}

func withDPoP(f *fakeOkta) { f.dpop = true }

func withData(logs, users []string) func(*fakeOkta) {
	return func(f *fakeOkta) { f.logs, f.users = logs, users }
}

func TestSSWS_Legacy(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(5), testUsers(5)))

	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if want := eventBase.Add(4 * time.Second); !o.latestTS.Equal(want) {
		t.Errorf("latestTS = %v, want %v", o.latestTS, want)
	}
	cursor, err := rt.GetString(urlKey)
	if err != nil || !strings.HasPrefix(cursor, testBaseURL+systemLogsPath+`?`) || !strings.Contains(cursor, `after=5`) {
		t.Errorf("saved cursor %q, %v", cursor, err)
	}

	rt2 := newTestRuntime(t)
	if _, err := o.getUsers(rt2.ctx, rt2, newUserTracker(), true); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt2.data(o.userTag), f.users)

	logQ, userQ, schemes, issued, _ := f.snapshot()
	assertSchemes(t, schemes, `SSWS`)
	if len(issued) != 0 {
		t.Error("SSWS path requested an OAuth token")
	}
	if len(logQ) != 3 || logQ[0].Get(`since`) != eventBase.Add(-time.Hour).Format(timeFormat) ||
		logQ[0].Get(`sortOrder`) != `ASCENDING` || logQ[0].Get(`limit`) != `2` || logQ[2].Get(`after`) != `4` {
		t.Errorf("unexpected system log queries %v", logQ)
	}
	if len(userQ) != 3 || userQ[0].Get(`search`) != allUsersSearch || userQ[0].Get(`limit`) != `200` {
		t.Errorf("unexpected user queries %v", userQ)
	}
}

// Token mode shares its fetch loops with OAuth mode: its errors carry the response status, and a
// system log failure flags the ingester status until a page succeeds
func TestSSWS_Errors(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(1), testUsers(1)))

	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	o.c = sswsDoer{next: o.c.(sswsDoer).next, token: `revoked`}
	if _, err := o.getUsers(rt.ctx, rt, newUserTracker(), true); err == nil || !isStatus(err, http.StatusUnauthorized) {
		t.Errorf("users error %v", err)
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("a users failure must not flag the error status: %v", serr)
	}
	if err := o.getSystemLogs(rt.ctx, rt); err == nil || !isStatus(err, http.StatusUnauthorized) || !strings.Contains(err.Error(), `Invalid token provided`) {
		t.Errorf("system logs error %v", err)
	}
	if _, serr := rt.Status(); serr == nil || !isStatus(serr, http.StatusUnauthorized) {
		t.Errorf("a system log failure did not flag the error status: %v", serr)
	}
	if len(rt.logged(``)) != 0 {
		t.Errorf("unexpected logs: %v", rt.logged(``))
	}

	o.c = sswsDoer{next: o.c.(sswsDoer).next, token: testSSWS}
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("error status not cleared: %v", serr)
	}
}

func TestOAuth_Bearer(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(5), testUsers(5)), func(f *fakeOkta) { f.kid = `override` })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, func(c *Config) {
		c.Private_Key = oktaJWK(t, jwkFields(testKey(), `jwk-kid`))
		c.Private_Key_ID = `override`
	})
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if want := eventBase.Add(4 * time.Second); !o.latestTS.Equal(want) {
		t.Errorf("latestTS = %v, want %v", o.latestTS, want)
	}
	if _, err := rt.Status(); err != nil {
		t.Errorf("error status not cleared: %v", err)
	}

	rt2 := newTestRuntime(t)
	if _, err := o.getUsers(rt2.ctx, rt2, newUserTracker(), true); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt2.data(o.userTag), f.users)

	logQ, userQ, schemes, issued, tokenErrs := f.snapshot()
	assertSchemes(t, schemes, `Bearer`)
	if len(issued) != 1 || len(tokenErrs) != 0 {
		t.Errorf("issued %d tokens with errors %v, want one shared by both routines", len(issued), tokenErrs)
	}
	if len(logQ) != 3 || logQ[0].Get(`since`) != eventBase.Add(-time.Hour).Format(timeFormat) ||
		logQ[0].Get(`sortOrder`) != `ASCENDING` || logQ[0].Get(`limit`) != `2` || logQ[0].Has(`after`) || logQ[2].Get(`after`) != `4` {
		t.Errorf("unexpected system log queries %v", logQ)
	}
	var afters []string
	for _, q := range userQ {
		afters = append(afters, q.Get(`after`))
		if q.Get(`search`) != allUsersSearch || q.Get(`limit`) != `200` {
			t.Errorf("unexpected user query %v", q)
		}
	}
	if !slices.Equal(afters, []string{``, `2`, `4`}) {
		t.Errorf("user pages requested after %q", afters)
	}
	assertClean(t, rt, f, false)
	assertClean(t, rt2, f, false)
}

// a key generated outside Okta: a PEM in Private-Key-File, and the kid its public JWK was imported with
func TestOAuth_KeyFile(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withDPoP, withData(testEvents(3), nil), func(f *fakeOkta) { f.kid = `gravwell-1` })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, func(c *Config) {
		c.Private_Key = ``
		c.Private_Key_File = writeKeyFile(t, pkcs8PEM(t, testKey()))
		c.Private_Key_ID = `gravwell-1`
	})
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	assertClean(t, rt, f, false)
}

// a key file that other users can access is worth a warning at startup, not a refusal to run
func TestOAuth_KeyFilePermissionWarning(t *testing.T) {
	t.Parallel()
	warned := func(t *testing.T, mode os.FileMode) bool {
		f := newFakeOkta(t, withData(testEvents(1), nil))
		rt := newTestRuntime(t)
		pth := writeKeyFile(t, pkcs8PEM(t, testKey()))
		if err := os.Chmod(pth, mode); err != nil {
			t.Fatal(err)
		}
		o := newOAuthIngester(t, f, rt, false, func(c *Config) { c.Private_Key, c.Private_Key_File = ``, pth })
		if err := o.Run(rt.ctx, rt); err != nil {
			t.Fatal(err)
		}
		assertData(t, rt.data(o.systemTag), f.logs)
		for _, l := range rt.logged(`WARN`) {
			if strings.Contains(l.msg, `Private-Key-File`) {
				return true
			}
		}
		return false
	}
	for mode, want := range map[os.FileMode]bool{0600: false, 0400: false, 0640: true, 0644: true} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			t.Parallel()
			if got := warned(t, mode); got != want {
				t.Errorf("warned = %v, want %v", got, want)
			}
		})
	}
}

func TestOAuth_DPoP(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withDPoP, withData(testEvents(4), testUsers(3)), func(f *fakeOkta) { f.kid = `jwk-kid` })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, func(c *Config) {
		c.Private_Key = oktaJWK(t, jwkFields(testKey(), `jwk-kid`))
	})
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)

	rt2 := newTestRuntime(t)
	if _, err := o.getUsers(rt2.ctx, rt2, newUserTracker(), true); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt2.data(o.userTag), f.users)

	_, _, schemes, issued, tokenErrs := f.snapshot()
	assertSchemes(t, schemes, `DPoP`)
	if len(issued) != 1 || !slices.Equal(tokenErrs, []string{`invalid_dpop_proof`, `use_dpop_nonce`}) {
		t.Errorf("issued %d tokens after %v, want one DPoP handshake", len(issued), tokenErrs)
	}
	if len(rt.logged(`DEBUG`)) == 0 {
		t.Error("DPoP handshake not logged at debug")
	}
	assertClean(t, rt, f, false)
	assertClean(t, rt2, f, false)
}

// TestOAuth_ResumeCursor resumes from a cursor saved by the Token path, through Run, then hands
// the cursor it saved back to the Token path
func TestOAuth_ResumeCursor(t *testing.T) {
	t.Parallel()
	events := testEvents(6)
	f := newFakeOkta(t, withData(events[:4], nil))

	rt := newTestRuntime(t)
	since := eventBase.Add(-2 * time.Hour).Format(timeFormat)
	legacy := testBaseURL + `/api/v1/logs?limit=2&sortOrder=ASCENDING&since=` + url.QueryEscape(since) + `&after=2`
	if err := rt.PutString(urlKey, legacy); err != nil {
		t.Fatal(err)
	}
	o := newOAuthIngester(t, f, rt, false, nil)
	if err := o.Run(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), events[2:4])
	logQ, _, _, _, _ := f.snapshot()
	want := url.Values{`after`: {`2`}, `limit`: {`2`}, `since`: {since}, `sortOrder`: {`ASCENDING`}}
	if len(logQ) == 0 || logQ[0].Encode() != want.Encode() {
		t.Fatalf("first query %v, want %v", logQ, want)
	}
	saved, err := rt.GetString(urlKey)
	if err != nil || !strings.HasPrefix(saved, testBaseURL+systemLogsPath+`?`) || !strings.Contains(saved, `after=4`) {
		t.Fatalf("saved cursor %q is not an absolute next link: %v", saved, err)
	}

	f.mu.Lock()
	f.logs = events
	f.mu.Unlock()
	rt2 := newTestRuntime(t)
	o2 := newSSWSIngester(t, f, rt2)
	if o2.systemLogsNext, err = url.ParseRequestURI(saved); err != nil { // as Run loads it
		t.Fatal(err)
	}
	if err := o2.getSystemLogs(rt2.ctx, rt2); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt2.data(o2.systemTag), events[4:])
}

// a rejected token is replaced and the request tried once more, in place
func TestOAuth_UnauthorizedRetriedWithNewToken(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withDPoP, withData(testEvents(3), nil), func(f *fakeOkta) { f.reject401 = 1 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if _, _, _, issued, _ := f.snapshot(); len(issued) != 2 {
		t.Errorf("issued %d tokens, want the rejected one and its replacement", len(issued))
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("error status set: %v", serr)
	}
	assertClean(t, rt, f, false)
}

// a second 401 in a row is reported, the next poll starts over with a new token
func TestOAuth_UnauthorizedTwice(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(3), nil), func(f *fakeOkta) { f.reject401 = 2 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	err := o.getSystemLogs(rt.ctx, rt)
	if !isStatus(err, http.StatusUnauthorized) || !strings.Contains(err.Error(), `Invalid token provided`) {
		t.Fatalf("expected a 401, got %v", err)
	}
	if _, serr := rt.Status(); serr == nil {
		t.Error("401 did not flag the error status")
	}

	if err = o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if _, _, _, issued, _ := f.snapshot(); len(issued) != 3 {
		t.Errorf("issued %d tokens, want a new one after each 401", len(issued))
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("error status not cleared: %v", serr)
	}
	assertClean(t, rt, f, true)
}

// events are ingested as Okta sent them, whatever their shape
func TestOAuth_EventsIngestedRaw(t *testing.T) {
	t.Parallel()
	ev := testEvents(2)
	logs := []string{ev[0], `{"uuid": "evt-bad",  "published": 12345, "actor": "not-an-object"}`, ev[1]}
	f := newFakeOkta(t, withData(logs, nil))

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, func(c *Config) { c.Request_Batch_Size = 3 })
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), logs)
	assertClean(t, rt, f, false)
}

func TestOAuth_MissingScope(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withDPoP, withData(testEvents(1), testUsers(2)), func(f *fakeOkta) { f.scope = `okta.logs.read` })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	_, err := o.getUsers(rt.ctx, rt, newUserTracker(), true)
	if !isInsufficientScope(err) || !strings.Contains(err.Error(), `insufficient_scope`) || !strings.Contains(err.Error(), `Read-only Administrator`) {
		t.Fatalf("expected an insufficient scope error, got %v", err)
	}
	var tokenWarn, usersWarn bool
	for _, l := range rt.logged(`WARN`) {
		for _, p := range l.params {
			if p.Name == `scope` && p.Value == `okta.users.read` {
				tokenWarn = true
			} else if p.Name == `scope` {
				t.Errorf("unexpected missing scope warning: %s", l)
			}
		}
		if strings.Contains(l.msg, `okta.users.read`) {
			usersWarn = true
		}
	}
	if !tokenWarn || !usersWarn {
		t.Errorf("missing scope warnings, token=%v users=%v: %v", tokenWarn, usersWarn, rt.logged(`WARN`))
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("a missing users scope must not flag the error status: %v", serr)
	}

	// the logs scope was granted, system logs keep flowing
	if err = o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	assertClean(t, rt, f, false)
}

// a scope granted after the first token was issued must take effect without waiting for that token to expire
func TestOAuth_ScopeGrantedLater(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(nil, testUsers(2)), func(f *fakeOkta) { f.scope = `okta.logs.read` })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	if _, err := o.getUsers(rt.ctx, rt, newUserTracker(), true); !isInsufficientScope(err) {
		t.Fatalf("expected an insufficient scope error, got %v", err)
	}

	f.mu.Lock()
	f.scope = bothScopes
	f.mu.Unlock()
	if _, err := o.getUsers(rt.ctx, rt, newUserTracker(), true); err != nil {
		t.Fatalf("users still failing after the scope was granted: %v", err)
	}
	assertData(t, rt.data(o.userTag), f.users)
	if _, _, _, issued, _ := f.snapshot(); len(issued) != 2 {
		t.Errorf("issued %d tokens, want the stale one and its replacement", len(issued))
	}
}

// a 429 in the middle of a users window is retried in place: the pages already written are not
// fetched again, the token is kept, and the retry carries a new DPoP proof (the fake rejects a replay)
func TestOAuth_RateLimitedRetriedInPlace(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withDPoP, withData(nil, testUsers(5)), func(f *fakeOkta) { f.skip429, f.reject429 = 1, 1 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	if _, err := o.getUsers(rt.ctx, rt, newUserTracker(), true); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.userTag), f.users)
	_, userQ, schemes, issued, _ := f.snapshot()
	if len(userQ) != 3 || len(schemes) != 4 || len(issued) != 1 {
		t.Errorf("%d pages served from %d requests with %d tokens, want 3 pages, 4 requests, 1 token", len(userQ), len(schemes), len(issued))
	}
	assertClean(t, rt, f, false)
}

// Token mode waits out a 429 too: the page is retried in place once Okta's rate limit resets
func TestSSWS_RateLimitedRetriedInPlace(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(nil, testUsers(5)), func(f *fakeOkta) { f.skip429, f.reject429 = 1, 2 })

	rt := newTestRuntime(t)
	o := newSSWSIngester(t, f, rt)
	o.c = sswsDoer{next: o.c.(sswsDoer).next, token: testSSWS, lg: rt}
	if _, err := o.getUsers(rt.ctx, rt, newUserTracker(), true); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.userTag), f.users)
	_, userQ, schemes, _, _ := f.snapshot()
	if len(userQ) != 3 || len(schemes) != 5 {
		t.Errorf("%d pages served from %d requests, want 3 pages, 5 requests", len(userQ), len(schemes))
	}
	var warned int
	for _, l := range rt.logged(`WARN`) {
		if strings.Contains(l.msg, `rate limit`) {
			warned++
		}
	}
	if warned != 2 {
		t.Errorf("warned %d times, want once per 429: %v", warned, rt.logged(``))
	}
	assertClean(t, rt, f, false)
}

func TestRateLimitWait(t *testing.T) {
	t.Parallel()
	okta := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) // Okta's clock, ours is elsewhere
	resp := func(reset, date string) *http.Response {
		h := http.Header{}
		if reset != `` {
			h.Set(`X-Rate-Limit-Reset`, reset)
		}
		if date != `` {
			h.Set(`Date`, date)
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h}
	}
	at := func(d time.Duration) string { return strconv.FormatInt(okta.Add(d).Unix(), 10) }
	date := okta.Format(http.TimeFormat)
	for name, c := range map[string]struct {
		resp *http.Response
		want time.Duration
	}{
		`reset ahead`:    {resp(at(30*time.Second), date), 30*time.Second + rateLimitPad},
		`reset now`:      {resp(at(0), date), rateLimitPad},
		`reset passed`:   {resp(at(-time.Minute), date), rateLimitPad},
		`reset far off`:  {resp(at(time.Hour), date), maxRateLimitWait},
		`no reset`:       {resp(``, date), 7 * time.Second},
		`bad reset`:      {resp(`soon`, date), 7 * time.Second},
		`zero reset`:     {resp(`0`, date), 7 * time.Second},
		`no date, ours`:  {resp(strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10), ``), rateLimitPad},
		`bad date, ours`: {resp(strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10), `yesterday`), rateLimitPad},
	} {
		if got := rateLimitWait(c.resp, 7*time.Second); got != c.want {
			t.Errorf("%s: waited %v, want %v", name, got, c.want)
		}
	}
}

// a token endpoint outage is retried in place, a refused credential is not
func TestOAuth_TokenEndpointRetried(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(1), nil), func(f *fakeOkta) { f.token503 = 2 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if _, _, _, issued, tokenErrs := f.snapshot(); len(issued) != 1 || !slices.Equal(tokenErrs, []string{`503`, `503`}) {
		t.Errorf("issued %d tokens after %v, want one after two 503s", len(issued), tokenErrs)
	}
}

// the token endpoint has its own rate limit: a 429 there is waited out until Okta's reset time, not
// retried on the short backoff, and logged as rate limiting rather than as a failed token request
func TestOAuth_TokenEndpointRateLimited(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(1), nil), func(f *fakeOkta) { f.token429 = 1 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil) // a millisecond backoff
	start := time.Now()
	if err := o.getSystemLogs(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < rateLimitPad {
		t.Errorf("retried after %v, want a wait for the rate limit reset", took)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	if _, _, _, issued, tokenErrs := f.snapshot(); len(issued) != 1 || !slices.Equal(tokenErrs, []string{`429`}) {
		t.Errorf("issued %d tokens after %v, want one after a 429", len(issued), tokenErrs)
	}
	var warned bool
	for _, l := range rt.logged(`WARN`) {
		warned = warned || (strings.Contains(l.msg, `rate limit`) && strings.Contains(l.String(), tokenPath))
	}
	if !warned {
		t.Errorf("token rate limiting not logged: %v", rt.logged(``))
	}
	assertClean(t, rt, f, false) // no error logged for it
}

// the saved cursor is an absolute URL, credentials must not follow it to another host
func TestOAuth_ForeignHostRefused(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(1), nil))

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	var err error
	if o.systemLogsNext, err = url.Parse(`http://example.okta.com.evil.net/api/v1/logs?after=1`); err != nil {
		t.Fatal(err)
	}
	if err = o.getSystemLogs(rt.ctx, rt); err == nil || !strings.Contains(err.Error(), `refusing`) {
		t.Fatalf("expected the request to be refused, got %v", err)
	}
	if _, _, schemes, issued, _ := f.snapshot(); len(schemes) != 0 || len(issued) != 0 {
		t.Errorf("%d API requests and %d tokens for a foreign host, want none", len(schemes), len(issued))
	}
}

// a cursor saved for another org is dropped at startup, collection restarts from the saved timestamp
func TestOAuth_ForeignCursorDropped(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(3), nil))

	rt := newTestRuntime(t)
	if err := rt.PutString(urlKey, `http://old-org.okta.com/api/v1/logs?limit=2&sortOrder=ASCENDING&after=2`); err != nil {
		t.Fatal(err)
	}
	if err := rt.PutTime(tsKey, eventBase.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	o := newOAuthIngester(t, f, rt, false, nil)
	if err := o.Run(rt.ctx, rt); err != nil {
		t.Fatal(err)
	}
	assertData(t, rt.data(o.systemTag), f.logs)
	logQ, _, _, _, _ := f.snapshot()
	if len(logQ) == 0 || logQ[0].Has(`after`) || logQ[0].Get(`since`) != eventBase.Add(-time.Hour).Format(timeFormat) {
		t.Errorf("first query %v, want a restart from the saved timestamp", logQ)
	}
	var warned bool
	for _, l := range rt.logged(`WARN`) {
		warned = warned || strings.Contains(l.msg, `discarding url checkpoint`)
	}
	if !warned {
		t.Errorf("the dropped cursor was not logged: %v", rt.logged(``))
	}
	if saved, err := rt.GetString(urlKey); err != nil || !strings.HasPrefix(saved, testBaseURL+systemLogsPath+`?`) {
		t.Errorf("saved cursor %q still points at another org: %v", saved, err)
	}
}

// the SDK follows use_dpop_nonce replies without a limit, the handshake must give up
func TestOAuth_DPoPNonceLoopBounded(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		opt  func(*fakeOkta)
		want int // token requests
	}{
		`nonce never accepted`:                {func(f *fakeOkta) { f.nonceLoop = true }, 2 + maxDPoPNonces},
		`no nonce header`:                     {func(f *fakeOkta) { f.nonceLoop, f.noNonce = true, true }, 2},
		`nonce named only in the description`: {func(f *fakeOkta) { f.nonceLoop, f.nonceInDesc = true, true }, 2 + maxDPoPNonces},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeOkta(t, withDPoP, withData(testEvents(1), nil), tt.opt)
			rt := newTestRuntime(t)
			o := newOAuthIngester(t, f, rt, true, nil)
			done := make(chan error, 1)
			go func() { done <- o.getSystemLogs(rt.ctx, rt) }()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), `nonce handshake did not complete`) {
					t.Fatalf("got %v, want the handshake abandoned", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the DPoP nonce handshake is still running")
			}
			if _, _, _, issued, tokenErrs := f.snapshot(); len(issued) != 0 || len(tokenErrs) != tt.want {
				t.Errorf("%d tokens after %d token requests %v, want none after %d", len(issued), len(tokenErrs), tokenErrs, tt.want)
			}
			if _, serr := rt.Status(); serr == nil {
				t.Error("the abandoned handshake did not flag the error status")
			}
		})
	}
}

// one routine must not flush a token the other has just fetched
func TestOAuthDoer_DropTokenOnlyIfRejected(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t)
	o := newOAuthIngester(t, f, newTestRuntime(t), true, nil)
	d := o.c.(*oauthDoer)
	cached := func() bool {
		_, ok := d.auth.TokenCache.Get(okta.AccessTokenCacheKey)
		return ok
	}
	d.auth.TokenCache.Set(okta.AccessTokenCacheKey, `Bearer current`, time.Minute)
	if d.dropToken(`Bearer stale`); !cached() {
		t.Fatal("flushed a token that had already replaced the rejected one")
	}
	if d.dropToken(`Bearer current`); cached() {
		t.Fatal("the rejected token was kept")
	}
}

// a 429 is not retried on the fixed backoff, every client waits for the rate limit reset instead
func TestRetryStatus(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]bool{425: true, 429: false, 500: true, 503: true, 200: false, 400: false, 401: false, 403: false, 404: false} {
		if got := retryStatus(status); got != want {
			t.Errorf("retryStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

// a shutdown ends the in place retries
func TestOAuth_ShutdownEndsRetries(t *testing.T) {
	t.Parallel()
	f := newFakeOkta(t, withData(testEvents(1), nil), func(f *fakeOkta) { f.reject429 = 1 << 30 })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, false, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := o.newOAuthDoer(ctx, rate.NewLimiter(rate.Inf, 1), rt)
	d.backoff = time.Hour
	o.c = d
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := o.getSystemLogs(ctx, rt); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the retries cancelled by shutdown", err)
	}
	if _, serr := rt.Status(); serr != nil {
		t.Errorf("a shutdown must not flag the error status: %v", serr)
	}
}

// nextHost is the host the fake puts in a next link, called with f.mu held
func (f *fakeOkta) nextHost(r *http.Request) string {
	if f.foreignNext {
		return `okta.example.net`
	}
	return r.Host
}

// a next link that leaves the org is neither followed nor saved, in either auth mode. Every
// connection in these tests reaches the fake, so a followed link would show up as another request.
func TestForeignNextLink(t *testing.T) {
	t.Parallel()
	ingesters := map[string]func(*testing.T, *fakeOkta, *testRuntime) *OktaIngester{
		`ssws`: newSSWSIngester,
		`oauth`: func(t *testing.T, f *fakeOkta, rt *testRuntime) *OktaIngester {
			return newOAuthIngester(t, f, rt, true, nil)
		},
	}
	for name, mk := range ingesters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeOkta(t, withData(testEvents(5), testUsers(5)), func(f *fakeOkta) { f.foreignNext = true })
			rt := newTestRuntime(t)
			o := mk(t, f, rt)

			err := o.getSystemLogs(rt.ctx, rt)
			if err == nil || !strings.Contains(err.Error(), `another host`) {
				t.Fatalf("system logs: got %v, want the next link refused", err)
			}
			assertData(t, rt.data(o.systemTag), f.logs[:2]) // the page itself is kept
			if o.systemLogsNext != nil {
				t.Errorf("the foreign link was kept as the cursor: %v", o.systemLogsNext)
			}
			if saved, _ := rt.GetString(urlKey); saved != `` {
				t.Errorf("the foreign link was saved: %q", saved)
			}
			if _, serr := rt.Status(); serr == nil {
				t.Error("the refused link did not flag the error status")
			}

			rt2 := newTestRuntime(t)
			_, err = o.getUsers(rt2.ctx, rt2, newUserTracker(), true)
			if err == nil || !strings.Contains(err.Error(), `another host`) {
				t.Fatalf("users: got %v, want the next link refused", err)
			}
			assertData(t, rt2.data(o.userTag), f.users[:2])
			if _, _, schemes, _, _ := f.snapshot(); len(schemes) != 2 {
				t.Fatalf("%d API requests, want one per call: nothing may follow the foreign links", len(schemes))
			}

			// with sane links again, collection restarts from the latest timestamp
			f.mu.Lock()
			f.foreignNext = false
			f.mu.Unlock()
			if err = o.getSystemLogs(rt.ctx, rt); err != nil {
				t.Fatal(err)
			}
			logQ, _, _, _, _ := f.snapshot()
			if q := logQ[1]; q.Has(`after`) || q.Get(`since`) != eventBase.Add(time.Second).Format(timeFormat) {
				t.Errorf("resumed with %v, want a restart from the latest timestamp", q)
			}
			if _, serr := rt.Status(); serr != nil {
				t.Errorf("error status not cleared: %v", serr)
			}
			if saved, _ := rt.GetString(urlKey); !strings.HasPrefix(saved, testBaseURL+systemLogsPath+`?`) {
				t.Errorf("saved cursor %q", saved)
			}
		})
	}
}

// timeFormat ends in a literal Z, so a host that is not on UTC must still send UTC times
func TestTimesSentAsUTC(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone(`UTC+2`, 2*60*60)
	since := eventBase.Add(-time.Hour)

	ingesters := map[string]func(*testing.T, *fakeOkta, *testRuntime) *OktaIngester{
		`ssws`: newSSWSIngester,
		`oauth`: func(t *testing.T, f *fakeOkta, rt *testRuntime) *OktaIngester {
			return newOAuthIngester(t, f, rt, true, nil)
		},
	}
	for name, mk := range ingesters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeOkta(t, withData(testEvents(1), nil))
			rt := newTestRuntime(t)
			o := mk(t, f, rt)
			o.latestTS = since.In(zone)
			if err := o.getSystemLogs(rt.ctx, rt); err != nil {
				t.Fatal(err)
			}
			logQ, _, _, _, _ := f.snapshot()
			if len(logQ) == 0 || logQ[0].Get(`since`) != `2026-10-01T11:00:00.000Z` {
				t.Errorf("unexpected system log queries %v", logQ)
			}
		})
	}
}

func TestOAuth_TokenFailureLogged(t *testing.T) {
	t.Parallel()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeOkta(t, func(f *fakeOkta) { f.pub = &other.PublicKey })

	rt := newTestRuntime(t)
	o := newOAuthIngester(t, f, rt, true, nil)
	// the status must say why Okta refused the token, the SDK reports only an empty token
	if err = o.getSystemLogs(rt.ctx, rt); err == nil || !strings.Contains(err.Error(), `invalid_client`) {
		t.Fatalf("expected an invalid_client error, got %v", err)
	}
	if _, _, _, _, tokenErrs := f.snapshot(); len(tokenErrs) != 1 {
		t.Errorf("%d token requests, a refused credential must not be retried", len(tokenErrs))
	}
	if _, serr := rt.Status(); serr == nil {
		t.Error("token failure did not flag the error status")
	}
	var logged bool
	for _, l := range rt.logged(`ERROR`) {
		for _, p := range l.params {
			logged = logged || (p.Name == `error` && p.Value == `invalid_client`)
		}
	}
	if !logged {
		t.Errorf("token endpoint error not logged: %v", rt.logged(``))
	}
	assertClean(t, rt, f, true)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// a low Request-Per-Minute waits on the limiter for longer than the request timeout, the wait must not count against it
func TestRateLimitTransport_TimeoutExcludesWait(t *testing.T) {
	t.Parallel()
	const wait, timeout = 200 * time.Millisecond, 50 * time.Millisecond
	var reqCtx context.Context
	cli := &http.Client{Transport: &rateLimitTransport{
		ctx:     t.Context(),
		rl:      rate.NewLimiter(rate.Every(wait), 1),
		timeout: timeout,
		next: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			reqCtx = r.Context()
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		}),
	}}
	start := time.Now()
	for i := range 2 { // the first request spends the burst, the second waits out the limiter
		resp, err := cli.Get(testBaseURL)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if reqCtx.Err() == nil {
			t.Errorf("request %d: closing the body did not release the request timeout", i)
		}
	}
	if d := time.Since(start); d < wait {
		t.Errorf("requests took %v, the limiter should have held the second for %v", d, wait)
	}
}

func TestRateLimitTransport_Timeout(t *testing.T) {
	t.Parallel()
	cli := &http.Client{Transport: &rateLimitTransport{
		ctx:     t.Context(),
		rl:      rate.NewLimiter(rate.Inf, 1),
		timeout: 20 * time.Millisecond,
		next: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}),
	}}
	if _, err := cli.Get(testBaseURL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want a deadline error", err)
	}
}

// the SDK builds token requests on context.Background(), a shutdown must still interrupt their limiter wait
func TestRateLimitTransport_ShutdownCancelsWait(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cli := &http.Client{Transport: &rateLimitTransport{
		ctx:     ctx,
		rl:      rate.NewLimiter(rate.Every(time.Hour), 1),
		timeout: time.Second,
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
	}}
	resp, err := cli.Get(testBaseURL) // spends the burst
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err = cli.Get(testBaseURL); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the wait cancelled by shutdown", err)
	}
}
