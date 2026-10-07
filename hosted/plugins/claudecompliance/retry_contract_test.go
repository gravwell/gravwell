package claudecompliance

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- Bounded retry ---
//
// The shared utils.RetryHttpClient cannot carry this contract: its Do loop
// (ingesters/utils/http.go:68-91) has no attempt bound and exits only on
// context cancellation, and it reads neither retry-after nor x-should-retry.
// Adopting it would lose that bound, so the plugin keeps its
// own loop and these tests pin the behavior the shared helper lacks.
//
// the plugin's own loop is bounded and honors both headers.
func TestPluginRetryIsBoundedAndHonorsHeaders(t *testing.T) {
	for _, tc := range []struct {
		name        string
		shouldRetry string
		retryAfter  string
		wantReqs    int
	}{
		{name: "x-should-retry false stops immediately", shouldRetry: "false", wantReqs: 1},
		{name: "bounded by Max-Retries", wantReqs: 4},
		{name: "over-ceiling retry-after ends the cycle", retryAfter: "99999", wantReqs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rt := setup(t, "activities")
			p.conf.Max_Retries = 3
			n := 0
			p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
				n++
				r := reply(`{}`, 503)
				if tc.shouldRetry != "" {
					r.Header.Set("x-should-retry", tc.shouldRetry)
				}
				if tc.retryAfter != "" {
					r.Header.Set("retry-after", tc.retryAfter)
				}
				return r, nil
			})
			_, e := p.Handle(t.Context(), rt)
			t.Logf("%s -> requests=%d sleeps=%v err=%v", tc.name, n, rt.sleeps, e)
			if n != tc.wantReqs {
				t.Errorf("requests=%d want %d", n, tc.wantReqs)
			}
			if e == nil {
				t.Error("expected the cycle to end with an error")
			}
		})
	}
}

// intrinsic EVs are the only thing separating 26 datasets that
// share 6 tags. Removing them makes datasets indistinguishable downstream.
func TestIntrinsicEVsAreLoadBearing(t *testing.T) {
	byTag := map[string][]string{}
	for name, d := range Datasets {
		byTag[d.Tag] = append(byTag[d.Tag], name)
	}
	shared := 0
	for tag, names := range byTag {
		if len(names) > 1 {
			shared++
			t.Logf("tag %q carries %d datasets: %v", tag, len(names), names)
		}
	}
	if shared == 0 {
		t.Fatal("no tag carries more than one dataset; _source would be redundant")
	}
	// Two datasets on one tag whose payloads are byte-identical are only
	// distinguishable by the intrinsic fields.
	pa, rta := setup(t, "group")
	pb, rtb := setup(t, "organization-settings")
	same := `{"id":"x"}`
	for _, pair := range []struct {
		p  *Plugin
		rt *runtime
	}{{pa, rta}, {pb, rtb}} {
		pair.p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
			return reply(same, 200), nil
		})
		if _, e := pair.p.Handle(t.Context(), pair.rt); e != nil {
			t.Fatal(e)
		}
	}
	if string(rta.entries[0].Data) != string(rtb.entries[0].Data) {
		t.Fatal("precondition: payloads should be identical")
	}
	if rta.entries[0].Tag != rtb.entries[0].Tag {
		t.Fatal("precondition: both should land on the directory tag")
	}
	sa, _ := rta.entries[0].GetEnumeratedValue("_source")
	sb, _ := rtb.entries[0].GetEnumeratedValue("_source")
	t.Logf("identical payload, identical tag, _source=%v vs %v", sa, sb)
	if sa == sb {
		t.Fatal("_source does not distinguish two datasets sharing a tag")
	}
}

// the response ceiling is a memory guard, and it is far above
// anything the documented page limits can produce.
func TestResponseCeilingIsFarAboveAnyRealPage(t *testing.T) {
	largest := 0
	var which string
	for name, d := range Datasets {
		if d.Limit > largest {
			largest, which = d.Limit, name
		}
	}
	// A generous per-record estimate against the largest documented page.
	const generousRecordBytes = 4096
	worst := largest * generousRecordBytes
	t.Logf("largest documented page: %s limit=%d; worst case at %dB/record = %dB; ceiling = %dB (%.1fx headroom)",
		which, largest, generousRecordBytes, worst, maxResponseBytes, float64(maxResponseBytes)/float64(worst))
	if maxResponseBytes <= worst {
		t.Fatalf("ceiling %d does not clear the worst documented page %d", maxResponseBytes, worst)
	}
	// And it is a guard against an unbounded read, not a data policy: prove
	// an endless body is stopped rather than read forever.
	p, rt := setup(t, "activities")
	p.maxEntryBytes = 1024
	read := 0
	p.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1,
			Body: endless(func(n int) { read += n })}, nil
	})
	_, e := p.Handle(t.Context(), rt)
	t.Logf("endless body: err=%v bytesRead=%d", e, read)
	if read > 2*maxResponseBytes {
		t.Fatalf("unbounded read: %d bytes", read)
	}
	_ = rt
}

type endlessReader struct{ onRead func(int) }

func (e endlessReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'a'
	}
	e.onRead(len(b))
	return len(b), nil
}
func (e endlessReader) Close() error { return nil }

func endless(onRead func(int)) endlessReader { return endlessReader{onRead} }

// a restart never walks back to the beginning of time. It
// resumes the stored cursor, and when there is none it starts at Lookback.
func TestRestartResumesRatherThanRewinding(t *testing.T) {
	p, rt := setup(t, "activities")
	p.conf.Lookback = 24
	var lows []string
	page := 0
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		lows = append(lows, r.URL.Query().Get("created_at.gte"))
		page++
		if page == 1 {
			return reply(`{"data":[{"id":"a1","created_at":"2026-09-07T12:00:00Z"}],"has_more":true,"last_id":"a1"}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	firstLow := lows[0]

	// A stored walk that has outlived the vendor cursor lifetime restarts --
	// but from its own stored lower bound, not from zero.
	p.conf.Max_Pages = 1
	page = 0
	rt2 := newRuntime(t)
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		lows = append(lows, r.URL.Query().Get("created_at.gte"))
		page++
		if page == 1 {
			return reply(`{"data":[{"id":"b","created_at":"2026-09-07T12:00:00Z"}],"has_more":true,"last_id":"b"}`, 200), nil
		}
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt2); e != nil {
		t.Fatal(e)
	}
	stored := readCheckpoint(t, rt2, p.conf.key())
	if stored.Walk == nil {
		t.Fatal("precondition: expected a stored walk")
	}
	start := p.now()
	p.now = func() time.Time { return start.Add(25 * time.Hour) }
	p.conf.Max_Pages = 5
	before := len(lows)
	if _, e := p.Handle(t.Context(), rt2); e != nil {
		t.Fatal(e)
	}
	restartLow := lows[before]
	t.Logf("cold start lower bound = %s; expired-walk restart lower bound = %s; walk stored lower bound = %s",
		firstLow, restartLow, stored.Walk.Since.Format(time.RFC3339Nano))
	if restartLow != stored.Walk.Since.Format(time.RFC3339Nano) {
		t.Fatalf("restart did not resume from the stored lower bound: %s", restartLow)
	}
	if strings.HasPrefix(restartLow, "0001-") || strings.HasPrefix(restartLow, "1970-") {
		t.Fatal("restart rewound to the beginning of time")
	}
}
