package mimecast

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		token        AuthToken
		clientId     string
		clientSecret string
		expectedErr  error
	}{
		{
			name:   "ok repsonse",
			status: http.StatusOK,
			token: AuthToken{
				AccessToken: "token",
				ExpireIn:    10,
			},
			clientId:     "client",
			clientSecret: "secret",
			expectedErr:  nil,
		},
		{
			name:         "token expired",
			status:       http.StatusUnauthorized,
			token:        AuthToken{},
			clientId:     "client",
			clientSecret: "secret",
			expectedErr:  ErrAuthenticationFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(authHandler(test.clientId, test.clientSecret, test.status, test.token))
			defer server.Close()

			client := NewClient(server.URL, test.clientId, test.clientSecret, server.Client())
			err := client.authenticate(t.Context())
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got error %v, want %v", err, test.expectedErr)
			}
		})
	}

	t.Run("only once", func(t *testing.T) {
		var count int
		counter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			w.WriteHeader(http.StatusOK)
			body, _ := json.Marshal(AuthToken{
				AccessToken: "token",
				ExpireIn:    10,
			})
			w.Write(body)
		})

		server := httptest.NewServer(counter)
		defer server.Close()
		client := NewClient(server.URL, "", "", server.Client())
		err := client.authenticate(t.Context())
		if err != nil {
			t.Errorf("got error %v, want nil", err)
		}
		// force a second auth, shouldn't actually hit server
		err = client.authenticate(t.Context())
		if err != nil {
			t.Errorf("got error %v, want nil", err)
		}
		if count != 1 {
			t.Errorf("client made %d requests, want 1", count)
		}
	})
}

func authHandler(id, secret string, status int, token AuthToken) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		err := r.ParseForm()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") != id {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_secret") != secret {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
		body, _ := json.Marshal(token)
		_, _ = w.Write(body)
	})
}
