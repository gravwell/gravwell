/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePrivateKey(t *testing.T) {
	t.Parallel()
	key := testKey()
	p1, p8 := pkcs1PEM(key), pkcs8PEM(t, key)

	indented := "\t" + strings.ReplaceAll(strings.TrimSpace(p8), "\n", "\n\t")
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a real certificate")}))
	tests := []struct {
		name    string
		raw     string
		wantKid string
	}{
		{name: "certificate then key bundle", raw: cert + p8},
		{name: "key then certificate bundle", raw: p1 + cert},
		{name: "pkcs1 pem", raw: p1},
		{name: "pkcs8 pem", raw: p8},
		{name: "literal newline escapes", raw: strings.ReplaceAll(p1, "\n", `\n`)},
		{name: "newlines collapsed to spaces", raw: strings.ReplaceAll(strings.TrimSpace(p8), "\n", " ")},
		{name: "indented lines", raw: indented},
		{name: "surrounding whitespace", raw: "\n  " + p1 + "  \n"},
		{name: "okta jwk", raw: oktaJWK(t, jwkFields(key, "kid-123")), wantKid: "kid-123"},
		{name: "jwk without kid", raw: oktaJWK(t, jwkFields(key, ""))},
		{name: "jwk set with one key", raw: `{"keys":[` + oktaJWK(t, jwkFields(key, "set-kid")) + `]}`, wantKid: "set-kid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, kid, err := parsePrivateKey(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kid != tt.wantKid {
				t.Errorf("kid = %q, want %q", kid, tt.wantKid)
			}
			if !out.Equal(key) {
				t.Fatal("output does not round trip to the input key")
			}
		})
	}
}

func TestParsePrivateKey_Rejects(t *testing.T) {
	t.Parallel()
	key := testKey()
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p8 := pkcs8PEM(t, key)
	keyFile := filepath.Join(t.TempDir(), "okta.pem")
	if err := os.WriteFile(keyFile, []byte(p8), 0600); err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode([]byte(p8))
	body := pemBody(p8)
	publicJWK := jwkFields(key, "kid")
	for _, f := range []string{"d", "p", "q", "dp", "dq", "qi"} {
		delete(publicJWK, f)
	}
	badCRT := jwkFields(key, "kid")
	badCRT["dp"] = badCRT["dq"]
	ecDER, err := x509.MarshalECPrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "garbage", raw: "dGhpcyBpcyBub3QgYSBrZXkgYXQgYWxsLCBqdXN0IGdhcmJhZ2U"},
		{name: "file path", raw: keyFile},
		{name: "relative file path", raw: "okta.pem"},
		{name: "ec pkcs8 pem", raw: pkcs8PEM(t, ec)},
		{name: "ec sec1 pem", raw: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER}))},
		{name: "ec jwk", raw: `{"kty":"EC","crv":"P-256","x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU","y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0","d":"jpsQnnGQmL-YBIffH1136cLyN0PeUUzWOpdP1xaMGhQ"}`},
		{name: "public jwk", raw: oktaJWK(t, publicJWK)},
		{name: "inconsistent jwk", raw: oktaJWK(t, badCRT)},
		{name: "jwk set with two keys", raw: `{"keys":[` + oktaJWK(t, jwkFields(key, "a")) + `,` + oktaJWK(t, jwkFields(key, "b")) + `]}`},
		{name: "empty jwk set", raw: `{"keys":[]}`},
		{name: "invalid json", raw: `{"kty":"RSA","d":"` + body[:64]},
		{name: "encrypted pkcs8 pem", raw: string(pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: block.Bytes}))},
		{name: "encrypted pkcs1 pem", raw: string(pem.EncodeToMemory(&pem.Block{
			Type:    "RSA PRIVATE KEY",
			Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,00112233445566778899AABBCCDDEEFF"},
			Bytes:   x509.MarshalPKCS1PrivateKey(key),
		}))},
		{name: "truncated pem", raw: "-----BEGIN PRIVATE KEY-----\n" + body[:len(body)/2] + "\n-----END PRIVATE KEY-----\n"},
		{name: "certificate", raw: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, kid, err := parsePrivateKey(tt.raw)
			if err == nil {
				t.Fatal("expected an error")
			}
			if out != nil || kid != "" {
				t.Error("rejected key returned output")
			}
			assertNoFragments(t, err.Error(), tt.raw, body, keyFile)
		})
	}
}

// a PEM with no private key in it, such as the public key or a certificate, must say so
func TestParsePrivateKey_NoKeyBlock(t *testing.T) {
	t.Parallel()
	pub, err := x509.MarshalPKIXPublicKey(&testKey().PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"public key":  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})),
		"certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a real certificate")})),
	} {
		if _, _, err := parsePrivateKey(raw); err == nil || !strings.Contains(err.Error(), "no private key PEM block found") {
			t.Errorf("%s: got %v, want it to say no private key was found", name, err)
		}
	}
}

// pemBody returns the base64 body of a PEM block without line breaks
func pemBody(p string) string {
	var sb strings.Builder
	for _, ln := range strings.Split(p, "\n") {
		if !strings.HasPrefix(ln, "-----") {
			sb.WriteString(strings.TrimSpace(ln))
		}
	}
	return sb.String()
}

// assertNoFragments fails if msg carries any 12 byte run of a secret's key material
func assertNoFragments(t *testing.T, msg string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		s = strings.NewReplacer(beginMarkerPrefix, " ", endMarkerPrefix, " ", "PRIVATE KEY-----", " ").Replace(s)
		for i := 0; i+12 <= len(s); i++ {
			frag := s[i : i+12]
			if strings.TrimSpace(frag) == frag && strings.Contains(msg, frag) {
				t.Fatalf("error leaks input bytes %q", frag)
			}
		}
	}
}
