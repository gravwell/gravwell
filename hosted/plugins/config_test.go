/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"testing"

	"github.com/gravwell/gravwell/v4/hosted/plugins/mqtt"
)

func mqttConfig(uuid, clientID string) *mqtt.Config {
	c := &mqtt.Config{
		Broker:    []string{`mqtt://127.0.0.1:1883`},
		Topic:     []string{`a/#`},
		Client_ID: clientID,
	}
	c.Tag_Name = `mqtt`
	c.Ingester_UUID = uuid
	return c
}

const (
	uuidA = `7db1995f-0000-0000-0000-00000000000a`
	uuidB = `7db1995f-0000-0000-0000-00000000000b`
)

// Two MQTT subscriptions must not present the same identity to a broker, and
// must not share an Ingester-UUID. Both collisions fail quietly at runtime:
// a shared client ID makes the two sessions evict each other forever, and a
// shared UUID makes the runner silently skip the second instance.
func TestConfigsRejectMQTTCollisions(t *testing.T) {
	tests := map[string]struct {
		a, b    *mqtt.Config
		wantErr bool
	}{
		"distinct is fine": {
			a: mqttConfig(uuidA, `one`), b: mqttConfig(uuidB, `two`),
		},
		"same client id": {
			a: mqttConfig(uuidA, `same`), b: mqttConfig(uuidB, `same`),
			wantErr: true,
		},
		"same ingester uuid": {
			a: mqttConfig(uuidA, `one`), b: mqttConfig(uuidA, `two`),
			wantErr: true,
		},
		"both unset uuid, which defaults to the same value": {
			a: mqttConfig(``, `one`), b: mqttConfig(``, `two`),
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := Configs{MQTT: map[string]*mqtt.Config{`a`: tt.a, `b`: tt.b}}
			err := c.Verify()
			if tt.wantErr && err == nil {
				t.Fatal("expected the collision to be rejected")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// A single MQTT config with no explicit Client-ID must still verify, the
// default is derived from the section name.
func TestConfigsSingleMQTTVerifies(t *testing.T) {
	c := Configs{MQTT: map[string]*mqtt.Config{`sensors`: mqttConfig(uuidA, ``)}}
	if err := c.Verify(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tags, err := c.Tags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0] != `mqtt` {
		t.Fatalf("unexpected tags %v", tags)
	}
}
