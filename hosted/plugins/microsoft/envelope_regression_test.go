package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListEnvelopeMustContainArray(t *testing.T) {
	for _, family := range []struct {
		kind  Kind
		field string
	}{{KindODataGET, "value"}, {KindResourceGraph, "data"}, {KindFabricActivity, "activityEventEntities"}} {
		var dataset Dataset
		for _, d := range datasets {
			if d.Kind == family.kind {
				dataset = d
				break
			}
		}
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{{"missing", `{}`, false}, {"null-body", `null`, false}, {"null-array", fmt.Sprintf(`{"%s":null}`, family.field), false}, {"wrong-type", fmt.Sprintf(`{"%s":{}}`, family.field), false}, {"empty", fmt.Sprintf(`{"%s":[]}`, family.field), true}} {
			t.Run(string(family.kind)+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/token") {
						fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
						return
					}
					fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				conf := testClientConfig(t, server.URL)
				conf.Subscription_ID = []string{"33333333-3333-3333-3333-333333333333"}
				client, err := NewClient(conf, nil)
				if err != nil {
					t.Fatal(err)
				}
				client.http.Transport = server.Client().Transport
				now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
				_, err = client.Fetch(context.Background(), dataset, now.Add(-time.Hour), now, conf.Subscription_ID[0])
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v error=%v", tc.valid, err)
				}
			})
		}
	}
}

func TestMalformedEnvelopeDoesNotAdvanceCheckpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"access_token":"synthetic","expires_in":3600}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	rt := newMicrosoftTestRuntime()
	plugin := newMicrosoftTestPlugin(t, server, rt)
	dataset := datasets["entra-directory-audits"]
	key := stateKey(plugin.conf.StateNamespace(), dataset.Name, "")
	before := streamState{Watermark: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC), Records: map[string]recordState{"old": {Hash: "retained"}}}
	if err := saveState(rt, key, before); err != nil {
		t.Fatal(err)
	}
	prior := append([]byte(nil), rt.values[key]...)
	if err := plugin.collect(context.Background(), rt, dataset, ""); err == nil {
		t.Error("malformed envelope accepted")
	}
	if string(rt.values[key]) != string(prior) {
		t.Fatal("malformed envelope advanced persisted checkpoint")
	}
}
