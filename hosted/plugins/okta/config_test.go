package okta

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/configtest"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestConfig_Verify(t *testing.T) {
	t.Parallel()

	invalidFallbackUUID := "not-a-uuid"

	tests := []struct {
		name     string
		cfg      Config
		wantErr  bool
		wantUUID string
	}{
		{
			name: "valid config",
			cfg: Config{
				Token:  "token",
				Domain: "example.okta.com",
			},
			wantErr:  false,
			wantUUID: "55af6d4e-3d04-431b-b860-b15b921a46c5",
		},
		{
			name: "valid with UUID",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: "550e8400-e29b-41d4-a716-446655440000"},
			},
			wantErr:  false,
			wantUUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "empty token",
			cfg: Config{
				Domain: "example.okta.com",
			},
			wantErr: true,
		},
		{
			name: "missing domain",
			cfg: Config{
				Token: "token",
			},
			wantErr: true,
		},
		{
			name: "invalid domain no suffix",
			cfg: Config{
				Token:  "token",
				Domain: "example.com",
			},
			wantErr: true,
		},
		{
			name: "valid preview domain",
			cfg: Config{
				Token:  "token",
				Domain: "example.oktapreview.com",
			},
			wantErr: false,
		},
		{
			name: "invalid look-alike domain",
			cfg: Config{
				Token:  "token",
				Domain: "example-notokta.com",
			},
			wantErr: true,
		},
		{
			name: "invalid fallback UUID",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: invalidFallbackUUID},
			},
			wantErr:  true,
			wantUUID: invalidFallbackUUID,
		},
		{
			name: "empty Ingester-UUID gets fallback",
			cfg: Config{
				Token:      "token",
				Domain:     "example.okta.com",
				BaseConfig: hosted.BaseConfig{Ingester_UUID: ""},
			},
			wantErr:  false,
			wantUUID: "55af6d4e-3d04-431b-b860-b15b921a46c5",
		},
		{
			name: "batch size too large",
			cfg: Config{
				Token:              "token",
				Domain:             "example.okta.com",
				Request_Batch_Size: 5000,
			},
			wantErr: true,
		},
		{
			name: "requests per minute too large",
			cfg: Config{
				Token:              "token",
				Domain:             "example.okta.com",
				Request_Per_Minute: 10000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantUUID != "" && tt.cfg.Ingester_UUID != tt.wantUUID {
				t.Errorf("Ingester_UUID = %q, want %q", tt.cfg.Ingester_UUID, tt.wantUUID)
			}
		})
	}
}

func TestConfig_Verify_SetsDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:  "token",
		Domain: "example.okta.com",
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Request_Batch_Size != 100 {
		t.Errorf("expected Request_Batch_Size %d, got %d", 100, cfg.Request_Batch_Size)
	}
	if cfg.Request_Per_Minute != 60 {
		t.Errorf("expected Request_Per_Minute %d, got %d", 60, cfg.Request_Per_Minute)
	}
	if cfg.Request_Burst != 10 {
		t.Errorf("expected Request_Burst %d, got %d", 10, cfg.Request_Burst)
	}
}

func TestConfig_Verify_PrefersExistingValues(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:              "token",
		Domain:             "example.okta.com",
		Request_Batch_Size: 200,
		Request_Per_Minute: 120,
		Request_Burst:      20,
	}
	if err := cfg.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Request_Batch_Size != 200 {
		t.Errorf("expected Request_Batch_Size 200, got %d", cfg.Request_Batch_Size)
	}
	if cfg.Request_Per_Minute != 120 {
		t.Errorf("expected Request_Per_Minute 120, got %d", cfg.Request_Per_Minute)
	}
	if cfg.Request_Burst != 20 {
		t.Errorf("expected Request_Burst 20, got %d", cfg.Request_Burst)
	}
}

func TestConfig_Verify_DomainContainsOkta(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Token:  "token",
		Domain: "sub.domain.okta.com",
	}
	err := cfg.Verify()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(cfg.Domain, "okta.com") {
		t.Error("domain should end with okta.com")
	}
}

func TestIsOktaDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		domain string
		want   bool
	}{
		{"example.okta.com", true},
		{"example-admin.okta.com", true},
		{"Example.Okta.com", true},
		{"example.oktapreview.com", true},
		{"example.okta-emea.com", true},
		{"example.okta-gov.com", true},
		{"example.okta.mil", true},
		{"okta.com", false},
		{"example-notokta.com", false},
		{"example.okta.com.evil.com", false},
		{"login.example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOktaDomain(tt.domain); got != tt.want {
			t.Errorf("isOktaDomain(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestConfigEqual(t *testing.T) {
	configtest.CheckEqual(t, Config{
		BaseConfig:         hosted.BaseConfig{Ingester_UUID: defaultIngesterUUIDStr},
		Request_Batch_Size: defaultPageSize,
		Request_Per_Minute: defaultRequestPerMinute,
		Request_Burst:      defaultRequestBurst,
		Domain:             "example.okta.com",
		Token:              "token",
		Client_ID:          "client",
		Private_Key:        "key",
		Private_Key_ID:     "kid",
	})
}

func TestConfig_Verify_Auth(t *testing.T) {
	t.Parallel()
	key := pkcs8PEM(t, testKey())

	tests := []struct {
		name      string
		cfg       Config
		wantErr   bool
		wantOAuth bool
	}{
		{name: "token", cfg: Config{Token: "token"}},
		{name: "oauth", cfg: Config{Client_ID: "client", Private_Key: key}, wantOAuth: true},
		{name: "oauth with key id", cfg: Config{Client_ID: "client", Private_Key: key, Private_Key_ID: "kid"}, wantOAuth: true},
		{name: "oauth jwk", cfg: Config{Client_ID: "client", Private_Key: oktaJWK(t, jwkFields(testKey(), "kid"))}, wantOAuth: true},
		{name: "neither", cfg: Config{}, wantErr: true},
		{name: "token and client id", cfg: Config{Token: "token", Client_ID: "client"}, wantErr: true},
		{name: "token and private key", cfg: Config{Token: "token", Private_Key: key}, wantErr: true},
		{name: "token and key id", cfg: Config{Token: "token", Private_Key_ID: "kid"}, wantErr: true},
		{name: "token and full oauth", cfg: Config{Token: "token", Client_ID: "client", Private_Key: key}, wantErr: true},
		{name: "client id only", cfg: Config{Client_ID: "client"}, wantErr: true},
		{name: "client id and key id", cfg: Config{Client_ID: "client", Private_Key_ID: "kid"}, wantErr: true},
		{name: "private key only", cfg: Config{Private_Key: key}, wantErr: true},
		{name: "private key and key id", cfg: Config{Private_Key: key, Private_Key_ID: "kid"}, wantErr: true},
		{name: "key id only", cfg: Config{Private_Key_ID: "kid"}, wantErr: true},
		{name: "invalid private key", cfg: Config{Client_ID: "client", Private_Key: "not-a-key"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.cfg.Domain = "example.okta.com"
			raw := tt.cfg.Private_Key
			err := tt.cfg.Verify()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.cfg.Private_Key != raw {
				t.Error("Verify mutated Private_Key")
			}
			if err == nil && tt.cfg.useOAuth() != tt.wantOAuth {
				t.Errorf("useOAuth() = %v, want %v", tt.cfg.useOAuth(), tt.wantOAuth)
			}
		})
	}
}

// Verify remembers the parsed key and NewOktaIngester reuses it, a changed Private_Key is parsed again
func TestConfig_PrivateKeyParsedOnce(t *testing.T) {
	t.Parallel()
	cfg := Config{Domain: "example.okta.com", Client_ID: "client", Private_Key: pkcs8PEM(t, testKey())}
	if err := cfg.Verify(); err != nil {
		t.Fatal(err)
	}
	first, _, err := cfg.privateKey()
	if err != nil || !first.Equal(testKey()) {
		t.Fatalf("privateKey() after Verify: %v", err)
	}

	// the runner verifies the config, then hands a copy to NewOktaIngester
	o, err := NewOktaIngester(cfg, newTestRuntime(t))
	if err != nil {
		t.Fatal(err)
	}
	if again, _, _ := o.cfg.privateKey(); again != first {
		t.Error("NewOktaIngester parsed the key again")
	}

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Private_Key = pkcs8PEM(t, other)
	if changed, _, err := cfg.privateKey(); err != nil || !changed.Equal(other) {
		t.Errorf("a changed Private_Key was not parsed again: %v", err)
	}
}

func TestConfig_Verify_InvalidKeyNotLeaked(t *testing.T) {
	t.Parallel()
	p8 := pkcs8PEM(t, testKey())
	body := pemBody(p8)
	raw := "-----BEGIN PRIVATE KEY-----\n" + body[:len(body)-40] + "\n-----END PRIVATE KEY-----"
	cfg := Config{Domain: "example.okta.com", Client_ID: "client", Private_Key: raw}
	err := cfg.Verify()
	if err == nil {
		t.Fatal("expected an error")
	}
	assertNoFragments(t, err.Error(), body)
}

func TestConfig_Verify_Domains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{domain: "example.okta.com"},
		{domain: "sub.domain.okta.com"},
		{domain: "example.oktapreview.com"},
		{domain: "example.okta-emea.com"},
		{domain: "example.okta-gov.com"},
		{domain: "example.okta.mil"},
		{domain: "okta.com", wantErr: true},
		{domain: "exampleokta.com", wantErr: true},
		{domain: "example.com", wantErr: true},
		{domain: "oktapreview.com", wantErr: true},
		{domain: "example.oktapreview.com.evil.net", wantErr: true},
		{domain: "example.okta.com.evil.net", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			t.Parallel()
			cfg := Config{Token: "token", Domain: tt.domain}
			if err := cfg.Verify(); (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfig_PrivateKeyForms covers the ways the README tells users to put a key in a gcfg file
func TestConfig_PrivateKeyForms(t *testing.T) {
	t.Parallel()
	key := testKey()
	p8 := strings.TrimSpace(pkcs8PEM(t, key))
	jwk := oktaJWK(t, jwkFields(key, "kid-1"))

	tests := []struct {
		name  string
		value string
	}{
		{name: "quoted pem with newline escapes", value: `"` + strings.ReplaceAll(p8, "\n", `\n`) + `"`},
		{name: "quoted single line pem", value: `"` + strings.ReplaceAll(p8, "\n", " ") + `"`},
		{name: "backtick multi-line pem", value: "`" + p8 + "`"},
		{name: "quoted jwk with escaped quotes", value: `"` + strings.ReplaceAll(jwk, `"`, `\"`) + `"`},
		{name: "backtick jwk", value: "`" + jwk + "`"},
		{name: "backtick jwk with comment", value: "`" + jwk + "` # okta jwk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conf := "[Okta \"test\"]\n\tDomain = example.okta.com\n\tClient-ID = client\n\tPrivate-Key = " + tt.value + "\n"
			var cr struct{ Okta map[string]*Config }
			if err := config.LoadConfigBytes(&cr, []byte(conf)); err != nil {
				t.Fatalf("failed to parse config: %v", err)
			}
			c := cr.Okta["test"]
			if c == nil {
				t.Fatal("okta section missing")
			}
			if err := c.Verify(); err != nil {
				t.Fatalf("Verify() = %v", err)
			}
			out, _, err := parsePrivateKey(c.Private_Key)
			if err != nil {
				t.Fatal(err)
			}
			if !out.Equal(key) {
				t.Error("parsed key does not match")
			}
		})
	}
}
