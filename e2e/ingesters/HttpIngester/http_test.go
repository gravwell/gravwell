package HttpIngester

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

func TestHttp(t *testing.T) {
	_, endpoint := setup(t, "http")
	t.Run("basic ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpNoAuth(t, endpoint+"/ingest", strings.NewReader(data))

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("basic auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpBasicAuth(t, endpoint+"/ingest/auth", "username:password", strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-auth", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("basic auth fails with bad username", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpBasicAuth(t, endpoint+"/ingest/auth", "blah:password", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("basic auth fails with bad password", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpBasicAuth(t, endpoint+"/ingest/auth", "username:blah", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("jwt auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		jwt := JwtLogin(t, endpoint+"/jwt/login", "username", "password", http.StatusOK)
		SendHttpJwtAuth(t, endpoint+"/jwt", jwt, strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-jwt", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("jwt login fails with bad username", func(t *testing.T) {
		JwtLogin(t, endpoint+"/jwt/login", "blah", "password", http.StatusForbidden)
	})
	t.Run("jwt login fails with bad password", func(t *testing.T) {
		JwtLogin(t, endpoint+"/jwt/login", "username", "blah", http.StatusForbidden)
	})
	t.Run("jwt auth fails with bad value", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpJwtAuth(t, endpoint+"/jwt", "my.fake.jwt", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("cookie auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		jwt := CookieLogin(t, endpoint+"/cookie/login", "username", "password", http.StatusOK)
		SendHttpCookieAuth(t, endpoint+"/cookie", jwt, strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-cookie", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("cookie login fails with bad username", func(t *testing.T) {
		CookieLogin(t, endpoint+"/cookie/login", "blah", "password", http.StatusForbidden)
	})
	t.Run("cookie login fails with bad password", func(t *testing.T) {
		CookieLogin(t, endpoint+"/cookie/login", "username", "blah", http.StatusForbidden)
	})
	t.Run("cookie auth fails with bad value", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpCookieAuth(t, endpoint+"/cookie", "my.fake.cookie", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("token auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpTokenAuth(t, endpoint+"/token", "Secret", strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-token", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("token auth fails with bad value", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpTokenAuth(t, endpoint+"/token", "blah", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("param auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpParamAuth(t, endpoint+"/param", "Secret", strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-param", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("param auth fails with bad value", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpParamAuth(t, endpoint+"/param", "blah", strings.NewReader(data), http.StatusUnauthorized)
	})
	t.Run("header auth ingest", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpHeaderAuth(t, endpoint+"/header", "Secret", strings.NewReader(data), http.StatusOK)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, "tag=http-header", time.Minute, 1, 30*time.Second), 1, data)
	})
	t.Run("header auth fails with bad value", func(t *testing.T) {
		data := `{"data": "passed"}`
		SendHttpHeaderAuth(t, endpoint+"/header", "blah", strings.NewReader(data), http.StatusUnauthorized)
	})
}

func sendHttp(t *testing.T, req *http.Request, expectedStatus int) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != expectedStatus {
		t.Fatalf("got status %d, expected %d", resp.StatusCode, expectedStatus)
	}
}

func SendHttpNoAuth(t *testing.T, endpoint string, body io.Reader) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	sendHttp(t, req, http.StatusOK)
}

func SendHttpBasicAuth(t *testing.T, endpoint, auth string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	sendHttp(t, req, expectedStatus)
}

func JwtLogin(t *testing.T, endpoint, username, password string, expectedStatus int) string {
	t.Helper()
	resp, err := http.PostForm(endpoint, url.Values{
		"username": []string{username},
		"password": []string{password},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != expectedStatus {
		t.Fatalf("got status %d, expected %d", resp.StatusCode, expectedStatus)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func SendHttpJwtAuth(t *testing.T, endpoint, jwt string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	sendHttp(t, req, expectedStatus)
}

func CookieLogin(t *testing.T, endpoint, username, password string, expectedStatus int) string {
	t.Helper()
	resp, err := http.PostForm(endpoint, url.Values{
		"username": []string{username},
		"password": []string{password},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != expectedStatus {
		t.Fatalf("got status %d, expected %d", resp.StatusCode, expectedStatus)
	}
	c := resp.Cookies()
	for _, cookie := range c {
		if cookie.Name == "_gravauth" {
			return cookie.Value
		}
	}

	return ""
}

func SendHttpCookieAuth(t *testing.T, endpoint, cookie string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  "_gravauth",
		Value: cookie,
	})
	sendHttp(t, req, expectedStatus)
}

func SendHttpTokenAuth(t *testing.T, endpoint, token string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Authorization", "Gravwell "+token)
	sendHttp(t, req, expectedStatus)
}

func SendHttpHeaderAuth(t *testing.T, endpoint, token string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Gravwell", token)
	sendHttp(t, req, expectedStatus)
}

func SendHttpParamAuth(t *testing.T, endpoint, token string, body io.Reader, expectedStatus int) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint+"?Gravwell="+token, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	sendHttp(t, req, expectedStatus)
}
