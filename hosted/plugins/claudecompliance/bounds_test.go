// Bounded configuration values: a setting that
// cannot be represented as a duration must be refused rather than silently
// wrapping, and a shared page size must be clamped to each endpoint's
// documented maximum on every execution path.
package claudecompliance

import (
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"
)

// A lookback beyond the representable range wraps negative on conversion and
// would place a window's lower bound after its upper bound.
func TestLookbackRepresentabilityBoundary(t *testing.T) {
	maxHours := int(math.MaxInt64 / int64(time.Hour))
	for _, tc := range []struct {
		hours int
		ok    bool
	}{{maxHours, true}, {maxHours + 1, false}} {
		p, _ := setup(t, "activities")
		c := *p.conf
		c.Lookback = tc.hours
		err := c.Verify()
		t.Logf("Lookback=%d -> %v", tc.hours, err)
		if tc.ok && err != nil {
			t.Errorf("representable boundary %d rejected: %v", tc.hours, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("unrepresentable Lookback %d accepted; window = %v",
				tc.hours, time.Duration(tc.hours)*time.Hour)
		}
	}
}

// Every execution path clamps Page-Size to the bound endpoint's documented
// maximum, including the single-dataset path that does not fan out.
func TestSingleDatasetClampsPageSizeToEndpointLimit(t *testing.T) {
	p, rt := setup(t, "projects")
	p.conf.Dataset = []string{"projects"}
	p.conf.Page_Size = 5000
	p.conf.Follow_Children = "disabled"
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	var limit string
	p.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		limit = r.URL.Query().Get("limit")
		return reply(`{"data":[],"has_more":false}`, 200), nil
	})
	if _, e := p.Handle(t.Context(), rt); e != nil {
		t.Fatal(e)
	}
	want := fmt.Sprint(Datasets["projects"].Limit)
	t.Logf("requested limit=%s want=%s", limit, want)
	if limit != want {
		t.Errorf("single-dataset path sent limit=%s, endpoint maximum is %s", limit, want)
	}
}

// A poll interval beyond the representable range wraps negative, which the
// runner reads as "run again immediately" -- an unbounded success loop.
func TestRequestIntervalRepresentabilityBoundary(t *testing.T) {
	maxSeconds := int(math.MaxInt64 / int64(time.Second))
	p, _ := setup(t, "activities")
	c := *p.conf
	c.Request_Interval = maxSeconds + 1
	err := c.Verify()
	cont := c.ContinueAfterInterval()
	t.Logf("Request-Interval=%d -> verify=%v continuation delay=%v", c.Request_Interval, err, cont.Delay)
	if err == nil && cont.Delay <= 0 {
		t.Errorf("Request-Interval %d accepted and wrapped to delay %v (immediate success loop)",
			c.Request_Interval, cont.Delay)
	}
}

// The representability boundary itself must be accepted, and one second
// past it rejected.
func TestRequestIntervalBoundaryIsExact(t *testing.T) {
	maxSeconds := int(math.MaxInt64 / int64(time.Second))
	for _, tc := range []struct {
		seconds int
		ok      bool
	}{{60, true}, {59, false}, {maxSeconds, true}, {maxSeconds + 1, false}} {
		p, _ := setup(t, "activities")
		c := *p.conf
		c.Request_Interval = tc.seconds
		err := c.Verify()
		if tc.ok && err != nil {
			t.Errorf("Request-Interval=%d rejected: %v", tc.seconds, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("Request-Interval=%d accepted", tc.seconds)
		}
		if tc.ok && err == nil && c.ContinueAfterInterval().Delay <= 0 {
			t.Errorf("Request-Interval=%d produced a non-positive delay", tc.seconds)
		}
	}
}

// Child configs are bound through the same path, so a discovered child also
// inherits the endpoint clamp rather than its parent's larger page size.
func TestDiscoveredChildInheritsEndpointPageSizeClamp(t *testing.T) {
	p, _ := setup(t, "projects")
	p.conf.Page_Size = 5000
	p.conf.Follow_Children = "enabled"
	if e := p.conf.Verify(); e != nil {
		t.Fatal(e)
	}
	if p.conf.Page_Size != Datasets["projects"].Limit {
		t.Fatalf("parent page size = %d, want the endpoint maximum %d", p.conf.Page_Size, Datasets["projects"].Limit)
	}
	child, e := childConfig(p.conf, work{
		Dataset:   "project-attachments",
		Parameter: []string{"project_id:p1"},
	})
	if e != nil {
		t.Fatal(e)
	}
	if want := Datasets["project-attachments"].Limit; child.Page_Size != want {
		t.Fatalf("child page size = %d, want %d", child.Page_Size, want)
	}
}
