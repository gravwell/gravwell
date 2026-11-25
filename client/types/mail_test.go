/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestUserMailConfigValidate(t *testing.T) {
	good := UserMailConfig{Server: `smtp.example.com`, Port: 587}
	tests := []struct {
		name string
		mod  func(*UserMailConfig)
		want error
	}{
		{"minimal", func(c *UserMailConfig) {}, nil},
		{"none", func(c *UserMailConfig) { c.Encryption = MailEncryptionNone }, nil},
		{"starttls", func(c *UserMailConfig) { c.Encryption = MailEncryptionStartTLS }, nil},
		{"tls", func(c *UserMailConfig) { c.Encryption = MailEncryptionTLS; c.Port = 465 }, nil},
		{"max port", func(c *UserMailConfig) { c.Port = 0xffff }, nil},
		{"no server", func(c *UserMailConfig) { c.Server = `` }, ErrMailNoServer},
		{"zero port", func(c *UserMailConfig) { c.Port = 0 }, ErrMailInvalidPort},
		{"negative port", func(c *UserMailConfig) { c.Port = -1 }, ErrMailInvalidPort},
		{"port too big", func(c *UserMailConfig) { c.Port = 0x10000 }, ErrMailInvalidPort},
		{"bad encryption", func(c *UserMailConfig) { c.Encryption = `ssl` }, ErrMailInvalidEncryption},
		{"wrong case", func(c *UserMailConfig) { c.Encryption = `TLS` }, ErrMailInvalidEncryption},
	}
	for _, tt := range tests {
		c := good
		tt.mod(&c)
		if err := c.Validate(); !errors.Is(err, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestUserMailConfigEncryptionMode(t *testing.T) {
	tests := []struct {
		useTLS     bool
		encryption string
		want       string
	}{
		{false, ``, MailEncryptionNone},
		{true, ``, MailEncryptionStartTLS},
		{false, MailEncryptionTLS, MailEncryptionTLS},
		{true, MailEncryptionNone, MailEncryptionNone}, // Encryption overrides UseTLS
		{true, MailEncryptionTLS, MailEncryptionTLS},
	}
	for _, tt := range tests {
		c := UserMailConfig{UseTLS: tt.useTLS, Encryption: tt.encryption}
		if got := c.EncryptionMode(); got != tt.want {
			t.Errorf("UseTLS=%v Encryption=%q: got %q, want %q", tt.useTLS, tt.encryption, got, tt.want)
		}
	}
}

func TestUserMailValidateConfig(t *testing.T) {
	msg := UserMail{From: `a@example.com`, To: []string{`b@example.com`}}
	if err := msg.Validate(); err != nil {
		t.Fatalf("no config: %v", err)
	}
	msg.Config = &UserMailConfig{Server: `smtp.example.com`, Port: 25}
	if err := msg.Validate(); err != nil {
		t.Fatalf("good config: %v", err)
	}
	msg.Config.Port = 0
	if err := msg.Validate(); !errors.Is(err, ErrMailInvalidPort) {
		t.Fatalf("bad config: got %v, want %v", err, ErrMailInvalidPort)
	}
}

func TestUserMailJSONCompat(t *testing.T) {
	// a message without Config must encode exactly as before, so older servers see no change
	b, err := json.Marshal(UserMail{From: `a@example.com`})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	} else if _, ok := m[`Config`]; ok {
		t.Fatalf("nil Config was encoded: %s", b)
	}

	// a stored config from before Encryption existed decodes to the legacy STARTTLS behavior
	var c UserMailConfig
	if err = json.Unmarshal([]byte(`{"Server":"smtp.example.com","Port":587,"UseTLS":true}`), &c); err != nil {
		t.Fatal(err)
	} else if c.EncryptionMode() != MailEncryptionStartTLS {
		t.Fatalf("legacy config: got %q", c.EncryptionMode())
	}
}
