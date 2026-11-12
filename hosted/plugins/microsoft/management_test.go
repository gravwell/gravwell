package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	stateStorage "github.com/gravwell/gravwell/v3/hosted/storage"
	"golang.org/x/time/rate"
)

var managementTestNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

const managementTestPublisher = "44444444-4444-4444-8444-444444444444"

type managementSyncController struct {
	calls  atomic.Int64
	failAt atomic.Int64
}

type persistentManagementRuntime struct {
	*microsoftTestRuntime
	state *stateStorage.BucketWriter
}

func (r *persistentManagementRuntime) Get(key string) ([]byte, error) { return r.state.Get(key) }
func (r *persistentManagementRuntime) Put(key string, value []byte) error {
	return r.state.Put(key, value)
}
func (r *persistentManagementRuntime) GetString(key string) (string, error) {
	return r.state.GetString(key)
}
func (r *persistentManagementRuntime) PutString(key, value string) error {
	return r.state.PutString(key, value)
}
func (r *persistentManagementRuntime) GetInt64(key string) (int64, error) {
	return r.state.GetInt64(key)
}
func (r *persistentManagementRuntime) PutInt64(key string, value int64) error {
	return r.state.PutInt64(key, value)
}
func (r *persistentManagementRuntime) GetTime(key string) (time.Time, error) {
	return r.state.GetTime(key)
}
func (r *persistentManagementRuntime) PutTime(key string, value time.Time) error {
	return r.state.PutTime(key, value)
}

func (c *managementSyncController) Sync() error {
	call := c.calls.Add(1)
	if failAt := c.failAt.Load(); failAt > 0 && call == failAt {
		return errors.New("synthetic state synchronization failure")
	}
	return nil
}

func newManagementTestPlugin(t *testing.T, server *httptest.Server, selector string, syncFn func() error) *Microsoft {
	t.Helper()
	conf := &Config{
		Tenant_ID:            "11111111-1111-1111-1111-111111111111",
		Client_ID:            "22222222-2222-2222-2222-222222222222",
		Client_Secret:        "synthetic-client-secret",
		Publisher_Identifier: managementTestPublisher,
		Api:                  []string{selector},
		Graph_Host:           server.URL,
		ARM_Host:             server.URL,
		Defender_Host:        server.URL,
		Defender_XDR_Host:    server.URL,
		PowerBI_Host:         server.URL,
		Management_Host:      server.URL,
		Auth_Host:            server.URL,
		PollingConfig: hosted.PollingConfig{
			Lookback: 24, Requests_Per_Minute: 6000, Request_Interval: 3600,
		},
	}
	if err := conf.Verify(); err != nil {
		t.Fatal(err)
	}
	plugin := NewWithStateSync(conf, syncFn)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.retryBackoff = 0
	plugin.client = client
	plugin.now = func() time.Time { return managementTestNow }
	return plugin
}

func managementTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			body, _ := io.ReadAll(r.Body)
			values, err := url.ParseQuery(string(body))
			if err != nil || values.Get("scope") != "https://"+r.Host+"/.default" || values.Get("client_secret") != "synthetic-client-secret" {
				t.Errorf("OAuth form=%q error=%v", body, err)
			}
			_, _ = io.WriteString(w, `{"access_token":"management-token","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer management-token" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		if got := r.URL.Query().Get("PublisherIdentifier"); got != managementTestPublisher {
			t.Errorf("PublisherIdentifier=%q", got)
		}
		handler(w, r)
	}))
}

func managementDescriptor(serverURL, contentType, contentID, contentPath string) string {
	return fmt.Sprintf(`{"contentType":%q,"contentId":%q,"contentUri":%q,"contentCreated":"2026-10-08T11:30:00Z","contentExpiration":"2026-10-15T11:30:00Z"}`,
		contentType, contentID, serverURL+contentPath)
}

func writeEnabledSubscription(w http.ResponseWriter, contentType string) {
	_, _ = fmt.Fprintf(w, `[{"contentType":%q,"status":"enabled"}]`, contentType)
}

func TestManagementActivityCollectsAllContentTypesAndRoutesCanonicalTags(t *testing.T) {
	tests := []struct {
		selector    string
		contentType string
		events      string
		wantTags    []string
	}{
		{"microsoft365-audit-azure-active-directory", "Audit.AzureActiveDirectory", `[{"Id":"aad-1","CreationTime":"2026-10-08T11:40:00Z","Operation":"UserLoggedIn"}]`, []string{"ms-o365-azure"}},
		{"microsoft365-audit-exchange", "Audit.Exchange", `[{"Id":"exchange-1","CreationTime":"2026-10-08T11:41:00Z","Operation":"MailItemsAccessed"}]`, []string{"ms-o365-exchange"}},
		{"microsoft365-audit-sharepoint", "Audit.SharePoint", `[{"Id":"sharepoint-1","CreationTime":"2026-10-08T11:42:00Z","Workload":"SharePoint"},{"Id":"onedrive-1","CreationTime":"2026-10-08T11:43:00Z","Workload":"OneDrive"}]`, []string{"ms-o365-sharepoint", "ms-o365-sharepoint"}},
		{"microsoft365-audit-general", "Audit.General", `[{"Id":"teams-1","CreationTime":"2026-10-08T11:44:00Z","Workload":"MicrosoftTeams"},{"Id":"general-1","CreationTime":"2026-10-08T11:45:00Z","Workload":"PowerBI"}]`, []string{"ms-o365-general", "ms-o365-general"}},
		{"microsoft365-dlp", "DLP.All", `[{"Id":"dlp-1","CreationTime":"2026-10-08T11:46:00Z","Operation":"DlpRuleMatch"}]`, []string{"ms-o365-dlp"}},
	}
	for _, tc := range tests {
		t.Run(tc.contentType, func(t *testing.T) {
			var server *httptest.Server
			var contentCalls atomic.Int64
			server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
					writeEnabledSubscription(w, tc.contentType)
				case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
					if r.URL.Query().Get("contentType") != tc.contentType {
						t.Errorf("contentType=%q", r.URL.Query().Get("contentType"))
					}
					start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
					end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("endTime"))
					if !start.Before(end) || end.Sub(start) > managementMaximumWindow || start.Before(managementTestNow.Add(-managementMaximumHistory)) {
						t.Errorf("window=[%s,%s)", start, end)
					}
					_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, tc.contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
				case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
					contentCalls.Add(1)
					_, _ = io.WriteString(w, tc.events)
				default:
					http.NotFound(w, r)
				}
			})
			defer server.Close()
			syncer := &managementSyncController{}
			runtime := newMicrosoftTestRuntime(t)
			plugin := newManagementTestPlugin(t, server, tc.selector, syncer.Sync)
			if continuation, err := plugin.Handle(t.Context(), runtime); err != nil {
				t.Fatal(err)
			} else if continuation == nil || continuation.Delay != time.Hour {
				t.Fatalf("continuation=%v", continuation)
			}
			entries := runtime.Entries()
			if len(entries) != len(tc.wantTags) || contentCalls.Load() != 1 {
				t.Fatalf("entries=%d content calls=%d", len(entries), contentCalls.Load())
			}
			for i, ent := range entries {
				if got := runtime.tagName(ent.Tag); got != tc.wantTags[i] {
					t.Fatalf("entry %d tag=%q want=%q", i, got, tc.wantTags[i])
				}
				if !json.Valid(ent.Data) || strings.ContainsAny(string(ent.Data), "\r\n") || !strings.Contains(string(ent.Data), `"Id"`) {
					t.Fatalf("entry %d framing=%q", i, ent.Data)
				}
			}
			state, err := loadManagementState(runtime, managementStateKey("microsoft", tc.selector))
			if err != nil || !state.CompletedThrough.Equal(managementTestNow) || len(state.Receipts) != 1 {
				t.Fatalf("state=%+v error=%v", state, err)
			}
		})
	}
}

func TestManagementEmptyCompletedPollSchedulesConfiguredInterval(t *testing.T) {
	var apiCalls atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	var clockCalls atomic.Int64
	plugin.now = func() time.Time {
		return managementTestNow.Add(time.Duration(clockCalls.Add(1)-1) * time.Second)
	}

	for cycle := 0; cycle < 5; cycle++ {
		continuation, err := plugin.Handle(t.Context(), runtime)
		if err != nil {
			t.Fatal(err)
		}
		if continuation == nil || continuation.Delay != time.Hour {
			t.Fatalf("cycle %d continuation=%v after %d API calls", cycle, continuation, apiCalls.Load())
		}
	}
}

func TestManagementCatchUpUsesOneFixedTargetAcrossBoundedWindows(t *testing.T) {
	var windows [][2]time.Time
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			start, startErr := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
			end, endErr := time.Parse(time.RFC3339, r.URL.Query().Get("endTime"))
			if startErr != nil || endErr != nil {
				t.Fatalf("window start=%q end=%q errors=%v %v", r.URL.Query().Get("startTime"), r.URL.Query().Get("endTime"), startErr, endErr)
			}
			windows = append(windows, [2]time.Time{start, end})
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	runtime := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.conf.Lookback = 48
	plugin.now = func() time.Time { return managementTestNow }

	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != 0 {
		t.Fatalf("first continuation=%v, want immediate catch-up", continuation)
	}
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil || continuation.Delay != time.Hour {
		t.Fatalf("second continuation=%v, want configured interval", continuation)
	}
	if len(windows) != 2 {
		t.Fatalf("windows=%v, want two bounded windows", windows)
	}
	wantStart := managementTestNow.Add(-48 * time.Hour)
	if !windows[0][0].Equal(wantStart) || !windows[0][1].Equal(wantStart.Add(24*time.Hour)) ||
		!windows[1][0].Equal(windows[0][1]) || !windows[1][1].Equal(managementTestNow) {
		t.Fatalf("windows=%v, want contiguous 24-hour windows ending at fixed target %v", windows, managementTestNow)
	}
	state, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.CompletedThrough.Equal(managementTestNow) || !state.TargetThrough.IsZero() || state.hasPending() {
		t.Fatalf("completed state=%+v", state)
	}
}

func TestManagementSubscriptionStartIsNotRepeatedInsideFifteenMinutes(t *testing.T) {
	const contentType = "Audit.Exchange"
	var starts atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			_, _ = io.WriteString(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/start"):
			starts.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("contentType") != contentType {
				t.Errorf("start method=%s query=%s", r.Method, r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatalf("subscription starts=%d, want 1", starts.Load())
	}
}

func TestManagementNextPageUriAndEveryContentURI(t *testing.T) {
	const contentType = "Audit.AzureActiveDirectory"
	var server *httptest.Server
	var listCalls, contentCalls atomic.Int64
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			call := listCalls.Add(1)
			if call == 1 {
				w.Header().Set("NextPageUri", server.URL+r.URL.Path+"?contentType="+url.QueryEscape(contentType)+"&page=2")
			}
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, fmt.Sprintf("content-%d", call), fmt.Sprintf("/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-%d", call)))
		case strings.Contains(r.URL.Path, "/audit/content-"):
			call := contentCalls.Add(1)
			_, _ = fmt.Fprintf(w, `[{"Id":"event-%d","CreationTime":"2026-10-08T11:50:00Z"}]`, call)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-azure-active-directory", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if listCalls.Load() != 2 || contentCalls.Load() != 2 || len(runtime.Entries()) != 2 {
		t.Fatalf("list=%d content=%d entries=%d", listCalls.Load(), contentCalls.Load(), len(runtime.Entries()))
	}
}

func TestManagementPartialFanoutResumesAfterWriteFailure(t *testing.T) {
	const contentType = "Audit.Exchange"
	var server *httptest.Server
	var contentCalls atomic.Int64
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s,%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"), managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			contentCalls.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"one","CreationTime":"2026-10-08T11:40:00Z"},{"Id":"two","CreationTime":"2026-10-08T11:41:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	runtime.failAt.Store(2)
	syncer := &managementSyncController{}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "write failure") {
		t.Fatalf("error=%v", err)
	}
	state, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil || state.EventIndex != 1 || len(state.Contents) != 1 || len(state.Receipts) != 0 || !state.CompletedThrough.IsZero() {
		t.Fatalf("partial state=%+v error=%v", state, err)
	}
	runtime.failAt.Store(0)
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.Entries()) != 2 || contentCalls.Load() != 2 {
		t.Fatalf("entries=%d content calls=%d", len(runtime.Entries()), contentCalls.Load())
	}
	state, err = loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil || len(state.Receipts) != 1 || !state.CompletedThrough.Equal(managementTestNow) {
		t.Fatalf("completed state=%+v error=%v", state, err)
	}
}

func TestManagementPaginationCycleAndPageCeilingFailClosed(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		const contentType = "Audit.Exchange"
		var server *httptest.Server
		var listCalls atomic.Int64
		server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				listCalls.Add(1)
				w.Header().Set("NextPageUri", server.URL+r.URL.RequestURI())
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		runtime := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "repeated") {
			t.Fatalf("error=%v", err)
		}
		if listCalls.Load() != 1 || len(runtime.Entries()) != 0 {
			t.Fatalf("list calls=%d entries=%d", listCalls.Load(), len(runtime.Entries()))
		}
	})

	t.Run("page-ceiling", func(t *testing.T) {
		const contentType = "Audit.Exchange"
		var contentListCalls atomic.Int64
		server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
				writeEnabledSubscription(w, contentType)
			case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
				contentListCalls.Add(1)
				_, _ = io.WriteString(w, `[]`)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()
		runtime := newMicrosoftTestRuntime(t)
		plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
		state := managementState{
			Version: managementStateVersion, WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow,
			ListingStarted: true, Pages: managementMaximumPagesPerWindow,
			NextPage: plugin.managementContentListURL(contentType, managementTestNow.Add(-time.Hour), managementTestNow),
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Put(managementStateKey("microsoft", "microsoft365-audit-exchange"), encoded); err != nil {
			t.Fatal(err)
		}
		if _, err := plugin.Handle(t.Context(), runtime); err != nil {
			t.Fatalf("page-ceiling recovery error=%v", err)
		}
		if contentListCalls.Load() != 1 || len(runtime.Entries()) != 0 {
			t.Fatalf("content list calls=%d entries=%d", contentListCalls.Load(), len(runtime.Entries()))
		}
		recovered, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
		if err != nil {
			t.Fatal(err)
		}
		want := managementTestNow.Add(-30 * time.Minute)
		if !recovered.CompletedThrough.Equal(want) || recovered.hasPending() {
			t.Fatalf("recovered state=%+v, want completed through %s with no pending listing", recovered, want)
		}
	})
}

func TestManagementPartialFanoutPersistsAcrossBoltReopen(t *testing.T) {
	const contentType = "Audit.Exchange"
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			_, _ = io.WriteString(w, `[{"Id":"one","CreationTime":"2026-10-08T11:40:00Z"},{"Id":"two","CreationTime":"2026-10-08T11:41:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	statePath := t.TempDir() + "/state.db"
	handler, err := stateStorage.OpenBoltHandler(statePath, false)
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := handler.GetBucketWriter("microsoft-test")
	if err != nil {
		t.Fatal(err)
	}
	rt1 := &persistentManagementRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), state: bucket}
	rt1.failAt.Store(2)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", bucket.Sync)
	if _, err := plugin.Handle(t.Context(), rt1); err == nil {
		t.Fatal("expected write failure")
	}
	if err := handler.Close(); err != nil {
		t.Fatal(err)
	}

	handler, err = stateStorage.OpenBoltHandler(statePath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	bucket, err = handler.GetBucketWriter("microsoft-test")
	if err != nil {
		t.Fatal(err)
	}
	rt2 := &persistentManagementRuntime{microsoftTestRuntime: newMicrosoftTestRuntime(t), state: bucket}
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-exchange", bucket.Sync)
	if _, err := plugin.Handle(t.Context(), rt2); err != nil {
		t.Fatal(err)
	}
	if len(rt1.Entries()) != 1 || len(rt2.Entries()) != 1 {
		t.Fatalf("before reopen entries=%d after reopen entries=%d", len(rt1.Entries()), len(rt2.Entries()))
	}
	state, err := loadManagementState(rt2, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil || !state.CompletedThrough.Equal(managementTestNow) || len(state.Receipts) != 1 {
		t.Fatalf("state=%+v error=%v", state, err)
	}
}

func TestManagementSyncFailureRollsBackProgressAndReplaysSafely(t *testing.T) {
	const contentType = "Audit.General"
	var server *httptest.Server
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			_, _ = io.WriteString(w, `[{"Id":"one","CreationTime":"2026-10-08T11:40:00Z","Workload":"PowerBI"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	syncer.failAt.Store(5)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-general", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "synchronize") {
		t.Fatalf("error=%v", err)
	}
	state, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-general"))
	if err != nil || state.EventIndex != 0 || len(state.Receipts) != 0 || !state.CompletedThrough.IsZero() {
		t.Fatalf("rolled-back state=%+v error=%v", state, err)
	}
	plugin = newManagementTestPlugin(t, server, "microsoft365-audit-general", syncer.Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.Entries()) != 2 {
		t.Fatalf("entries=%d, want one accepted entry plus one bounded replay", len(runtime.Entries()))
	}
}

func TestManagementExpiredContentURLIsRelistedByContentID(t *testing.T) {
	const contentType = "Audit.SharePoint"
	var server *httptest.Server
	var lists, oldCalls, newCalls atomic.Int64
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			path := "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/old"
			if lists.Add(1) > 1 {
				path = "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/new"
			}
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "stable-content-id", path))
		case strings.HasSuffix(r.URL.Path, "/audit/old"):
			oldCalls.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"AF20050","message":"expired secret-like body"}}`)
		case strings.HasSuffix(r.URL.Path, "/audit/new"):
			newCalls.Add(1)
			_, _ = io.WriteString(w, `[{"Id":"refreshed","CreationTime":"2026-10-08T11:40:00Z","Workload":"OneDrive"}]`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-sharepoint", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	if lists.Load() != 2 || oldCalls.Load() != 1 || newCalls.Load() != 1 || len(runtime.Entries()) != 1 {
		t.Fatalf("lists=%d old=%d new=%d entries=%d", lists.Load(), oldCalls.Load(), newCalls.Load(), len(runtime.Entries()))
	}
}

func TestManagementRetries429And5xxWithBoundedAttempts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstStatus int
		retryAfter  string
	}{
		{"rate-limit", http.StatusTooManyRequests, "0"},
		{"server-error", http.StatusInternalServerError, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const contentType = "DLP.All"
			var server *httptest.Server
			var attempts atomic.Int64
			server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
					writeEnabledSubscription(w, contentType)
				case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
					_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
				case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
					if attempts.Add(1) == 1 {
						if tc.retryAfter != "" {
							w.Header().Set("Retry-After", tc.retryAfter)
						}
						w.WriteHeader(tc.firstStatus)
						_, _ = io.WriteString(w, `{"error":{"code":"transient"}}`)
						return
					}
					_, _ = io.WriteString(w, `[{"Id":"event","CreationTime":"2026-10-08T11:40:00Z"}]`)
				default:
					http.NotFound(w, r)
				}
			})
			defer server.Close()
			runtime := newMicrosoftTestRuntime(t)
			plugin := newManagementTestPlugin(t, server, "microsoft365-dlp", (&managementSyncController{}).Sync)
			if _, err := plugin.Handle(t.Context(), runtime); err != nil {
				t.Fatal(err)
			}
			if attempts.Load() != 2 || len(runtime.Entries()) != 1 {
				t.Fatalf("attempts=%d entries=%d", attempts.Load(), len(runtime.Entries()))
			}
		})
	}
}

func TestManagementTerminalRetryIsBoundedAndKeepsCheckpoint(t *testing.T) {
	const contentType = "Audit.Exchange"
	var server *httptest.Server
	var attempts atomic.Int64
	server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, contentType)
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
		case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
			attempts.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"AF50000"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("expected terminal retry error")
	}
	if attempts.Load() != managementMaximumAttempts || len(runtime.Entries()) != 0 {
		t.Fatalf("attempts=%d entries=%d", attempts.Load(), len(runtime.Entries()))
	}
	state, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
	if err != nil || len(state.Receipts) != 0 || !state.CompletedThrough.IsZero() {
		t.Fatalf("state=%+v error=%v", state, err)
	}
}

func TestManagementMalformedAndOversizedContentNeverAdvances(t *testing.T) {
	tests := []struct {
		name string
		body func() string
		skip bool
	}{
		{"object-not-array", func() string { return `{"Id":"wrong-envelope"}` }, false},
		{"malformed", func() string { return `[{` }, false},
		{"oversized-event", func() string {
			return `[{"Id":"large","payload":"` + strings.Repeat("x", managementMaximumEntrySize+1) + `"}]`
		}, true},
		{"too-many-events", func() string {
			return `[` + strings.Repeat(`{},`, managementMaximumEventsPerContent) + `{}` + `]`
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const contentType = "Audit.Exchange"
			var server *httptest.Server
			server = managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
					writeEnabledSubscription(w, contentType)
				case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
					_, _ = fmt.Fprintf(w, "[%s]", managementDescriptor(server.URL, contentType, "content-1", "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/audit/content-1"))
				case strings.HasSuffix(r.URL.Path, "/audit/content-1"):
					_, _ = io.WriteString(w, tc.body())
				default:
					http.NotFound(w, r)
				}
			})
			defer server.Close()
			runtime := newMicrosoftTestRuntime(t)
			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			_, handleErr := plugin.Handle(t.Context(), runtime)
			if tc.skip && handleErr != nil {
				t.Fatalf("oversized event should be skipped: %v", handleErr)
			}
			if !tc.skip && handleErr == nil {
				t.Fatal("expected content failure")
			}
			state, err := loadManagementState(runtime, managementStateKey("microsoft", "microsoft365-audit-exchange"))
			if err != nil || len(runtime.Entries()) != 0 {
				t.Fatalf("state=%+v entries=%d error=%v", state, len(runtime.Entries()), err)
			}
			if tc.skip && (state.SkippedEvents != 1 || state.CompletedThrough.IsZero()) {
				t.Fatalf("skipped state=%+v, want one skipped event and completed window", state)
			}
			if !tc.skip && (len(state.Receipts) != 0 || !state.CompletedThrough.IsZero()) {
				t.Fatalf("terminal state advanced: %+v", state)
			}
		})
	}
}

func TestManagementInlineSecretIsUsedOnTokenAcquisition(t *testing.T) {
	var secrets []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		secrets = append(secrets, values.Get("client_secret"))
		_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`)
	}))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	if _, err := client.token(t.Context(), client.conf.Management_Scope); err != nil {
		t.Fatal(err)
	}
	conf.Client_Secret = "rotated-synthetic-secret"
	delete(client.tokens, client.conf.Management_Scope)
	if _, err := client.token(t.Context(), client.conf.Management_Scope); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(secrets, []string{"synthetic-client-secret", "rotated-synthetic-secret"}) {
		t.Fatalf("loaded secrets=%q", secrets)
	}
}

func TestManagementImmediateContinuationsAreBounded(t *testing.T) {
	runtime := newMicrosoftTestRuntime(t)
	plugin := NewWithStateSync(&Config{}, func() error { return nil })
	key := managementStateKey("microsoft", "bounded")
	state := managementState{Version: managementStateVersion, WindowStart: managementTestNow.Add(-time.Hour), WindowEnd: managementTestNow}
	for i := 1; i <= managementMaximumImmediateRuns; i++ {
		pending, err := plugin.managementContinuation(runtime, key, &state)
		if err != nil {
			t.Fatal(err)
		}
		if pending != (i < managementMaximumImmediateRuns) {
			t.Fatalf("run=%d pending=%v", i, pending)
		}
	}
	loaded, err := loadManagementState(runtime, key)
	if err != nil || loaded.ImmediateRuns != 0 {
		t.Fatalf("state=%+v error=%v", loaded, err)
	}
}

func TestManagementRequestBudgetStopsBeforeDispatch(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	conf := testClientConfig(t, server.URL)
	conf.Management_Host = server.URL
	client, err := NewClient(conf, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	client.tokens[client.conf.Management_Scope] = cachedToken{Value: "token", Expires: time.Now().Add(time.Hour)}
	ctx := context.WithValue(t.Context(), fetchBudgetKey{}, &fetchBudget{requestsRemaining: 0})
	endpoint := server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/list"
	if _, err := client.managementRequest(ctx, http.MethodGet, endpoint); !errors.Is(err, errFetchBudget) {
		t.Fatalf("error=%v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("requests=%d", calls.Load())
	}
}

func TestManagementRetryAfterIsSharedAcrossSelectorsAndRestart(t *testing.T) {
	var calls atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	syncer := &managementSyncController{}
	now := managementTestNow
	newPlugin := func() *Microsoft {
		plugin := newManagementTestPlugin(t, server, "microsoft365-dlp", syncer.Sync)
		plugin.conf.Api = []string{"microsoft365-dlp", "microsoft365-audit-exchange"}
		plugin.client.fetchDeadline = 25 * time.Millisecond
		plugin.now = func() time.Time { return now }
		return plugin
	}

	plugin := newPlugin()
	continuation, err := plugin.Handle(t.Context(), runtime)
	if err != nil || continuation == nil || continuation.Delay <= 0 || continuation.Delay > time.Second {
		t.Fatalf("continuation=%v error=%v", continuation, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("first Handle dispatched %d Management requests, want 1", calls.Load())
	}
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil || continuation == nil || continuation.Delay <= 0 || calls.Load() != 1 {
		t.Fatalf("second Handle continuation=%v calls=%d error=%v", continuation, calls.Load(), err)
	}
	plugin = newPlugin()
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil || continuation == nil || continuation.Delay <= 0 || calls.Load() != 1 {
		t.Fatalf("recreated plugin continuation=%v calls=%d error=%v", continuation, calls.Load(), err)
	}

	now = now.Add(2 * time.Second)
	plugin = newPlugin()
	continuation, err = plugin.Handle(t.Context(), runtime)
	if err != nil || continuation == nil || continuation.Delay <= 0 || calls.Load() != 2 {
		t.Fatalf("eligible plugin continuation=%v calls=%d error=%v", continuation, calls.Load(), err)
	}
}

func TestManagementFinalAttemptPreservesRetryAfter(t *testing.T) {
	var calls atomic.Int64
	server := managementTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == managementMaximumAttempts {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer server.Close()
	plugin := newManagementTestPlugin(t, server, "microsoft365-dlp", (&managementSyncController{}).Sync)
	plugin.client.retryBackoff = 0
	endpoint := plugin.managementFeedURL("subscriptions/list", nil)
	_, err := plugin.client.managementRequest(t.Context(), http.MethodGet, endpoint)
	var deferred *managementDeferredError
	if !errors.As(err, &deferred) || deferred.Delay <= 0 {
		t.Fatalf("error=%v", err)
	}
	if calls.Load() != managementMaximumAttempts {
		t.Fatalf("requests=%d", calls.Load())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, err := plugin.client.managementRequest(ctx, http.MethodGet, endpoint); !errors.As(err, &deferred) {
		t.Fatalf("second error=%v", err)
	}
	if calls.Load() != managementMaximumAttempts {
		t.Fatalf("in-memory eligibility dispatched request %d", calls.Load())
	}
}

func TestManagementRetentionFloorAppliesToInitialAndOldCheckpointWindows(t *testing.T) {
	tests := []struct {
		name  string
		state managementState
	}{
		{name: "initial-168-hour-lookback"},
		{name: "old-checkpoint", state: managementState{Version: managementStateVersion, CompletedThrough: managementTestNow.Add(-10 * 24 * time.Hour)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var starts []time.Time
			server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
					writeEnabledSubscription(w, "Audit.Exchange")
				case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
					start, err := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
					if err != nil {
						t.Fatal(err)
					}
					starts = append(starts, start)
					_, _ = io.WriteString(w, `[]`)
				default:
					http.NotFound(w, r)
				}
			})
			defer server.Close()
			runtime := newMicrosoftTestRuntime(t)
			key := managementStateKey("microsoft", "microsoft365-audit-exchange")
			if tc.state.Version != 0 {
				encoded, err := json.Marshal(tc.state)
				if err != nil {
					t.Fatal(err)
				}
				if err := runtime.Put(key, encoded); err != nil {
					t.Fatal(err)
				}
			}
			plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
			plugin.conf.Lookback = 168
			plugin.now = func() time.Time { return managementTestNow }
			if _, err := plugin.Handle(t.Context(), runtime); err != nil {
				t.Fatal(err)
			}
			want := managementTestNow.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin)
			if len(starts) != 1 || !starts[0].Equal(want) {
				t.Fatalf("starts=%v want=%s", starts, want)
			}
		})
	}
}

func TestManagementRestartRebasesStalePreListingWindowWithoutAdvancingCheckpoint(t *testing.T) {
	var starts []time.Time
	server := managementTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions/list"):
			writeEnabledSubscription(w, "Audit.Exchange")
		case strings.HasSuffix(r.URL.Path, "/subscriptions/content"):
			start, err := time.Parse(time.RFC3339, r.URL.Query().Get("startTime"))
			if err != nil {
				t.Fatal(err)
			}
			starts = append(starts, start)
			http.Error(w, "synthetic listing failure", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	runtime := newMicrosoftTestRuntime(t)
	key := managementStateKey("microsoft", "microsoft365-audit-exchange")
	plannedAt := managementTestNow.Add(-10 * time.Minute)
	completed := managementTestNow.Add(-8 * 24 * time.Hour)
	staleStart := plannedAt.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin)
	state := managementState{
		Version:          managementStateVersion,
		CompletedThrough: completed,
		TargetThrough:    plannedAt,
		WindowStart:      staleStart,
		WindowEnd:        staleStart.Add(managementMaximumWindow),
		ListingStarted:   true,
		Pages:            1,
		NextPage:         server.URL + "/api/v1.0/11111111-1111-1111-1111-111111111111/activity/feed/subscriptions/content?contentType=Audit.Exchange&PublisherIdentifier=" + managementTestPublisher,
		Contents: []managementContent{{
			ContentType: "Audit.Exchange", ContentID: "partial", ContentURI: server.URL + "/partial", ContentCreated: staleStart.Add(time.Hour),
		}},
		Receipts: []managementReceipt{{ContentID: "acknowledged-prior", ContentCreated: managementTestNow.Add(-time.Hour)}},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Put(key, encoded); err != nil {
		t.Fatal(err)
	}
	plugin := newManagementTestPlugin(t, server, "microsoft365-audit-exchange", (&managementSyncController{}).Sync)
	plugin.now = func() time.Time { return managementTestNow }
	if _, err := plugin.Handle(t.Context(), runtime); err == nil {
		t.Fatal("expected synthetic listing failure")
	}
	loaded, err := loadManagementState(runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := managementTestNow.Add(-managementMaximumHistory).Add(managementRetentionDispatchMargin)
	if len(starts) != 1 || !starts[0].Equal(wantStart) {
		t.Fatalf("starts=%v want=%s", starts, wantStart)
	}
	if !loaded.CompletedThrough.Equal(completed) || !loaded.WindowStart.Equal(wantStart) {
		t.Fatalf("completed=%s window-start=%s", loaded.CompletedThrough, loaded.WindowStart)
	}
	if loaded.ListingStarted || loaded.Pages != 0 || loaded.NextPage != "" || len(loaded.Contents) != 0 {
		t.Fatalf("listing state was not reset: %+v", loaded)
	}
	if len(loaded.Receipts) != 1 || loaded.Receipts[0].ContentID != "acknowledged-prior" {
		t.Fatalf("receipts=%+v", loaded.Receipts)
	}
}
