package main

import (
	"fmt"
	"strings"
	"sync"
)

// Result tracks the outcome of publishing events to a single event hub.
type Result struct {
	Resource string
	Sent     int
	Failed   int
	Err      error
}

// Summary holds all results from a run. It is safe for concurrent use.
type Summary struct {
	mu      sync.Mutex
	Results []Result
}

// Add appends a result to the summary.
func (s *Summary) Add(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Results = append(s.Results, r)
}

// String formats the summary for display.
func (s *Summary) String() string {
	if len(s.Results) == 0 {
		return "No results."
	}

	var b strings.Builder
	b.WriteString("\n=== Results ===\n")

	for _, r := range s.Results {
		status := "✓"
		detail := fmt.Sprintf("%d sent", r.Sent)
		if r.Err != nil {
			status = "✗"
			detail = fmt.Sprintf("error: %v", r.Err)
		} else if r.Failed > 0 {
			status = "!"
			detail = fmt.Sprintf("%d sent, %d failed", r.Sent, r.Failed)
		}
		fmt.Fprintf(&b, "  %s %s — %s\n", status, r.Resource, detail)
	}

	return b.String()
}
