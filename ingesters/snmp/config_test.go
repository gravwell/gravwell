/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

const (
	testAuthPass    = `authPassMarker`
	testPrivacyPass = `privacyPassMarker`
	testCommunity   = `communityMarker`
	testIngestPass  = `ingestSecretMarker`

	secretConfig = `
[Global]
Ingest-Secret = ` + testIngestPass + `
Cleartext-Backend-Target=127.0.0.1:4023
Log-Level=INFO

[Listener "v2"]
	Tag-Name=snmpv2
	Bind-String="0.0.0.0:162"
	Version=2c
	Community=` + testCommunity + `

[Listener "v3"]
	Tag-Name=snmpv3
	Bind-String="0.0.0.0:163"
	Version=3
	Username=myuser
	Auth-Passphrase=` + testAuthPass + `
	Auth-Protocol=MD5
	Privacy-Passphrase=` + testPrivacyPass + `
	Privacy-Protocol=DES
`

	aesConfig = `
[Global]
Ingest-Secret = ` + testIngestPass + `
Cleartext-Backend-Target=127.0.0.1:4023
Log-Level=INFO

[Listener "v3"]
	Tag-Name=snmpv3
	Bind-String="0.0.0.0:163"
	Version=3
	Username=myuser
	Auth-Passphrase=` + testAuthPass + `
	Auth-Protocol=sha-256   #inline comment
	Privacy-Passphrase=` + testPrivacyPass + `
	Privacy-Protocol=aes-256   #inline comment
`

	v2cStrayConfig = `
[Global]
Ingest-Secret = ` + testIngestPass + `
Cleartext-Backend-Target=127.0.0.1:4023
Log-Level=INFO

[Listener "v2"]
	Tag-Name=snmpv2
	Bind-String="0.0.0.0:161"
	Version=2c
	Community=public
	Privacy-Protocol=DES
`
)

// TestConfigSecretsNotReported makes sure the SNMP secrets are loaded from the
// config but are not in the JSON handed to SetRawConfiguration.
func TestConfigSecretsNotReported(t *testing.T) {
	p := filepath.Join(t.TempDir(), `snmp.conf`)
	if err := os.WriteFile(p, []byte(secretConfig), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(p, ``)
	if err != nil {
		t.Fatal(err)
	}

	// make sure the secrets actually got loaded
	if l, ok := cfg.Listener[`v2`]; !ok {
		t.Fatal("missing v2 listener")
	} else if l.Community != testCommunity {
		t.Fatalf("bad community: %q", l.Community)
	}
	if l, ok := cfg.Listener[`v3`]; !ok {
		t.Fatal("missing v3 listener")
	} else if l.Auth_Passphrase != testAuthPass || l.Privacy_Passphrase != testPrivacyPass {
		t.Fatalf("bad v3 passphrases: %q %q", l.Auth_Passphrase, l.Privacy_Passphrase)
	}

	// marshal the same way SetRawConfiguration does
	bts, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)
	for _, marker := range []string{testAuthPass, testPrivacyPass, testCommunity, testIngestPass} {
		if strings.Contains(s, marker) {
			t.Fatalf("reported config contains secret %q: %s", marker, s)
		}
	}
	for _, want := range []string{`snmpv2`, `snmpv3`, `0.0.0.0:163`, `myuser`} {
		if !strings.Contains(s, want) {
			t.Fatalf("reported config is missing %q: %s", want, s)
		}
	}
}

// TestAuthProtocols makes sure every auth protocol the config accepts maps to
// the right gosnmp value and that junk is rejected.
func TestAuthProtocols(t *testing.T) {
	tests := []struct {
		name string
		want gosnmp.SnmpV3AuthProtocol
	}{
		{``, gosnmp.NoAuth},
		{`MD5`, gosnmp.MD5},
		{`md5`, gosnmp.MD5},
		{`SHA`, gosnmp.SHA},
		{`SHA1`, gosnmp.SHA},
		{`sha-1`, gosnmp.SHA},
		{`SHA224`, gosnmp.SHA224},
		{`SHA256`, gosnmp.SHA256},
		{`sha-256`, gosnmp.SHA256},
		{`SHA384`, gosnmp.SHA384},
		{`SHA512`, gosnmp.SHA512},
	}
	for _, test := range tests {
		v := v3auth{Auth_Protocol: test.name}
		if err := v.validate(true); err != nil {
			t.Fatalf("auth protocol %q rejected: %v", test.name, err)
		}
		if got := v.getAuthProto(); got != test.want {
			t.Fatalf("auth protocol %q mapped to %v, want %v", test.name, got, test.want)
		}
	}
	for _, name := range []string{`SHA2`, `AES`, `SHA 256`, `nope`} {
		v := v3auth{Auth_Protocol: name}
		if err := v.validate(true); err == nil {
			t.Fatalf("auth protocol %q was accepted", name)
		}
	}
}

// TestPrivacyProtocols makes sure every privacy protocol the config accepts
// maps to the right gosnmp value and that junk is rejected.
func TestPrivacyProtocols(t *testing.T) {
	tests := []struct {
		name string
		want gosnmp.SnmpV3PrivProtocol
	}{
		{``, gosnmp.NoPriv},
		{`DES`, gosnmp.DES},
		{`des`, gosnmp.DES},
		{`AES`, gosnmp.AES},
		{`aes`, gosnmp.AES},
		{`AES128`, gosnmp.AES},
		{`aes-128`, gosnmp.AES},
		{`AES192`, gosnmp.AES192},
		{`aes-192`, gosnmp.AES192},
		{`AES256`, gosnmp.AES256},
		{`AES192C`, gosnmp.AES192C},
		{`aes-192-c`, gosnmp.AES192C},
		{`AES256C`, gosnmp.AES256C},
	}
	for _, test := range tests {
		// privacy requires auth, so pair it up
		v := v3auth{Auth_Protocol: `SHA`, Privacy_Protocol: test.name}
		if err := v.validate(true); err != nil {
			t.Fatalf("privacy protocol %q rejected: %v", test.name, err)
		}
		if got := v.getPrivacyProto(); got != test.want {
			t.Fatalf("privacy protocol %q mapped to %v, want %v", test.name, got, test.want)
		}
	}
	for _, name := range []string{`AES512`, `MD5`, `AES 256`, `nope`} {
		v := v3auth{Auth_Protocol: `SHA`, Privacy_Protocol: name}
		if err := v.validate(true); err == nil {
			t.Fatalf("privacy protocol %q was accepted", name)
		}
	}
	// privacy without auth is not a thing in SNMPv3
	v := v3auth{Privacy_Protocol: `AES`}
	if err := v.validate(true); err == nil {
		t.Fatal("privacy without auth was accepted")
	}
}

// TestPrivacyWithoutAuthOnlyAppliesToV3 makes sure the privacy-requires-auth
// rule does not fire on a v2c listener, where the v3 security settings are
// inert and have always been ignored.  A typo in a protocol name is still an
// error on any version.
func TestPrivacyWithoutAuthOnlyAppliesToV3(t *testing.T) {
	v := v3auth{Privacy_Protocol: `DES`}
	if err := v.validate(false); err != nil {
		t.Fatalf("stray Privacy-Protocol rejected on a non-v3 listener: %v", err)
	}
	if err := (&v3auth{Privacy_Protocol: `nope`}).validate(false); err == nil {
		t.Fatal("invalid Privacy-Protocol was accepted on a non-v3 listener")
	}
	if err := (&v3auth{Auth_Protocol: `nope`}).validate(false); err == nil {
		t.Fatal("invalid Auth-Protocol was accepted on a non-v3 listener")
	}
}

// TestConfigV2cStrayPrivacyProtocol makes sure a working v2c config that
// carries a leftover Privacy-Protocol still loads.  Those lines never did
// anything on a v2c listener and breaking them would be a silent upgrade
// failure in the field.
func TestConfigV2cStrayPrivacyProtocol(t *testing.T) {
	p := filepath.Join(t.TempDir(), `snmp.conf`)
	if err := os.WriteFile(p, []byte(v2cStrayConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(p, ``); err != nil {
		t.Fatalf("v2c config with a stray Privacy-Protocol was rejected: %v", err)
	}
}

// TestMsgFlags makes sure the msgFlags track the configured protocols, the
// trap handler drops traps whose flags do not match.
func TestMsgFlags(t *testing.T) {
	tests := []struct {
		auth string
		priv string
		want gosnmp.SnmpV3MsgFlags
	}{
		{``, ``, gosnmp.NoAuthNoPriv},
		{`SHA256`, ``, gosnmp.AuthNoPriv},
		{`SHA256`, `AES`, gosnmp.AuthPriv},
		{`md5`, `aes-256`, gosnmp.AuthPriv},
	}
	for _, test := range tests {
		v := v3auth{Auth_Protocol: test.auth, Privacy_Protocol: test.priv}
		if got := v.getMsgFlags(); got != test.want {
			t.Fatalf("msgFlags for %q/%q is %v, want %v", test.auth, test.priv, got, test.want)
		}
	}
}

// TestConfigPrivacyProtocol makes sure a privacy protocol set in an actual
// config file survives the parser, inline comments and all.
func TestConfigPrivacyProtocol(t *testing.T) {
	p := filepath.Join(t.TempDir(), `snmp.conf`)
	if err := os.WriteFile(p, []byte(aesConfig), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := GetConfig(p, ``)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := cfg.Listener[`v3`]
	if !ok {
		t.Fatal("missing v3 listener")
	}
	if got := l.getPrivacyProto(); got != gosnmp.AES256 {
		t.Fatalf("privacy protocol is %v, want %v", got, gosnmp.AES256)
	}
	if got := l.getAuthProto(); got != gosnmp.SHA256 {
		t.Fatalf("auth protocol is %v, want %v", got, gosnmp.SHA256)
	}
	if got := l.getMsgFlags(); got != gosnmp.AuthPriv {
		t.Fatalf("msgFlags is %v, want %v", got, gosnmp.AuthPriv)
	}
}
