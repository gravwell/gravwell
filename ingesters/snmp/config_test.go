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
