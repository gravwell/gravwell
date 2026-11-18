/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strings"
)

const (
	beginMarkerPrefix = "-----BEGIN "
	endMarkerPrefix   = "-----END "
)

const maxKeyFileSize = 64 * 1024 // far above any RSA key in PEM or JWK form

var errKeyEncoding = errors.New(`unrecognized private key encoding: expected PEM ("-----BEGIN ...") or Okta JWK JSON ("{...}")`)

// readKeyFile reads a Private-Key-File. Errors name the path and never contain the contents.
func readKeyFile(pth string) ([]byte, error) {
	check := func(fi os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if !fi.Mode().IsRegular() {
			return fmt.Errorf("%q is not a regular file", pth)
		} else if fi.Size() > maxKeyFileSize {
			return fmt.Errorf("%q is too large to be a private key", pth)
		}
		return nil
	}
	// check before opening, opening a FIFO blocks until something writes to it
	if err := check(os.Stat(pth)); err != nil {
		return nil, err
	}
	fin, err := os.Open(pth)
	if err != nil {
		return nil, err
	}
	defer fin.Close()
	// and again on the open file, so the checks hold for the file that is read
	if err = check(fin.Stat()); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(fin, maxKeyFileSize))
	// editors on Windows put a byte order mark in front of a JWK
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), err
}

// parsePrivateKey parses the text of an Okta JWK or RSA PEM private key, returning the JWK kid if any.
// A path is not key material and is rejected, errors never contain key material.
func parsePrivateKey(raw string) (key *rsa.PrivateKey, kid string, err error) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))

	switch {
	case strings.HasPrefix(s, "{"):
		key, kid, err = parseJWKPrivateKey([]byte(s))
	case strings.Contains(s, beginMarkerPrefix):
		if key, err = parsePEMPrivateKey([]byte(s)); err != nil {
			// line structure is the usual casualty of carrying a PEM through a config value
			if repaired, rerr := repairPEM(s); rerr == nil {
				if k, perr := parsePEMPrivateKey([]byte(repaired)); perr == nil {
					key, err = k, nil
				}
			}
			if err != nil {
				err = fmt.Errorf("%w (%s)", err, pemShape(s))
			}
		}
	default:
		err = errKeyEncoding
	}
	if err != nil {
		return nil, "", err
	}
	return key, kid, nil
}

// repairPEM rebuilds a PEM block whose line structure was lost (collapsed newlines, indentation)
func repairPEM(s string) (string, error) {
	const markerSuffix = "-----"

	bi := strings.Index(s, beginMarkerPrefix)
	if bi < 0 {
		return "", errors.New("no BEGIN marker")
	}
	afterBegin := s[bi+len(beginMarkerPrefix):]

	typeEnd := strings.Index(afterBegin, markerSuffix)
	if typeEnd < 0 {
		return "", errors.New("BEGIN marker is not terminated")
	}
	blockType := strings.TrimSpace(afterBegin[:typeEnd])
	if blockType == "" {
		return "", errors.New("BEGIN marker has no block type")
	}

	body := afterBegin[typeEnd+len(markerSuffix):]
	endIdx := strings.Index(body, endMarkerPrefix)
	if endIdx < 0 {
		return "", errors.New("no END marker")
	}

	var b64 strings.Builder
	for _, r := range body[:endIdx] {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			b64.WriteRune(r)
		}
	}

	der, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		return "", errors.New("base64 body is not decodable")
	}

	out := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if out == nil {
		return "", errors.New("PEM re-encoding failed")
	}
	return string(out), nil
}

// pemShape describes key material structure (markers and counts only) for decode errors
func pemShape(s string) string {
	indented := false
	for _, ln := range strings.Split(s, "\n") {
		if ln != strings.TrimLeft(ln, " \t") {
			indented = true
			break
		}
	}
	return fmt.Sprintf("lines=%d begin_marker=%t end_marker=%t indented=%t chars=%d",
		strings.Count(s, "\n")+1,
		strings.Contains(s, beginMarkerPrefix),
		strings.Contains(s, endMarkerPrefix),
		indented,
		len(s),
	)
}

// parsePEMPrivateKey accepts PKCS#1 ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") PEM.
func parsePEMPrivateKey(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("PEM decode failed: no PEM block found")
	}
	if strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
		return nil, errors.New("encrypted PEM private keys are not supported; supply an unencrypted key")
	}

	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1 private key: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is %T, but Okta private_key_jwt requires an RSA key", k)
		}
		key = rk
	case "ENCRYPTED PRIVATE KEY":
		return nil, errors.New("encrypted PEM private keys are not supported; supply an unencrypted key")
	default:
		return nil, errors.New(`unsupported PEM block type, expected "RSA PRIVATE KEY" or "PRIVATE KEY"`)
	}

	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid RSA private key: %w", err)
	}
	return key, nil
}

// jwkRSAPrivateKey is Okta's JWK shape: base64url values, no padding.
type jwkRSAPrivateKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	D   string `json:"d"`
	P   string `json:"p"`
	Q   string `json:"q"`
	Dp  string `json:"dp"`
	Dq  string `json:"dq"`
	Qi  string `json:"qi"`
}

func parseJWKPrivateKey(b []byte) (*rsa.PrivateKey, string, error) {
	var jwk jwkRSAPrivateKey
	if err := json.Unmarshal(b, &jwk); err != nil {
		// deliberately not wrapped, a JSON error can echo bytes of the input
		return nil, "", errors.New("JWK decode failed: value is not valid JSON")
	}
	if jwk.Kty != "" && !strings.EqualFold(jwk.Kty, "RSA") {
		return nil, "", errors.New("JWK kty is not RSA, Okta private_key_jwt requires an RSA key")
	}

	required := []struct{ name, val string }{
		{"n", jwk.N}, {"e", jwk.E}, {"d", jwk.D}, {"p", jwk.P}, {"q", jwk.Q},
	}
	for _, f := range required {
		if f.val == "" {
			return nil, "", fmt.Errorf("JWK is missing required RSA field %q; Okta emits the full private JWK, a public JWK will not work", f.name)
		}
	}

	n, err := jwkBigInt("n", jwk.N)
	if err != nil {
		return nil, "", err
	}
	e, err := jwkBigInt("e", jwk.E)
	if err != nil {
		return nil, "", err
	}
	if !e.IsInt64() || e.Int64() < 3 || e.Int64() > math.MaxInt32 {
		return nil, "", errors.New(`JWK field "e" is not a supported RSA public exponent`)
	}
	d, err := jwkBigInt("d", jwk.D)
	if err != nil {
		return nil, "", err
	}
	p, err := jwkBigInt("p", jwk.P)
	if err != nil {
		return nil, "", err
	}
	q, err := jwkBigInt("q", jwk.Q)
	if err != nil {
		return nil, "", err
	}

	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	key.Precompute()
	if err := key.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid RSA private key from JWK: %w", err)
	}

	// the CRT values are recomputed from p and q above, compare so a truncated or
	// hand-edited JWK fails here rather than at the token endpoint
	crt := []struct {
		name string
		val  string
		want *big.Int
	}{
		{"dp", jwk.Dp, key.Precomputed.Dp},
		{"dq", jwk.Dq, key.Precomputed.Dq},
		{"qi", jwk.Qi, key.Precomputed.Qinv},
	}
	for _, f := range crt {
		if f.val == "" {
			continue
		}
		got, err := jwkBigInt(f.name, f.val)
		if err != nil {
			return nil, "", err
		}
		if got.Cmp(f.want) != 0 {
			return nil, "", fmt.Errorf("JWK field %q disagrees with the value derived from p and q; the key material is inconsistent", f.name)
		}
	}

	return key, jwk.Kid, nil
}

func jwkBigInt(field, val string) (*big.Int, error) {
	// tolerate padded base64url even though the JWK spec forbids the padding
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(val, "="))
	if err != nil {
		return nil, fmt.Errorf("JWK field %q is not valid base64url", field)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("JWK field %q is empty", field)
	}
	return new(big.Int).SetBytes(b), nil
}
