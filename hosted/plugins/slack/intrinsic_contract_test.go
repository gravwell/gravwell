package slack

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// intrinsicDecisions is the code-side half of the frozen producer/consumer
// matrix. The current Slack kit parses the compact JSON body and does not
// consume plugin-emitted enumerated values. Its _vendor, _product, _category,
// and _subcategory values are constants created by the kit's alert macro.
var intrinsicDecisions = []intrinsicDecision{
	{name: "_vendor", reason: "kit alert-macro constant"},
	{name: "_product", reason: "kit alert-macro constant"},
	{name: "_category", reason: "kit alert-macro constant"},
	{name: "_subcategory", reason: "kit alert-macro constant"},
	{name: "_source", reason: "no current Slack Audit kit consumer"},
	{name: "_recordType", reason: "no current Slack Audit kit consumer"},
	{name: "_endpoint", reason: "no current Slack Audit kit consumer"},
	{name: "_apiVersion", reason: "no current Slack Audit kit consumer"},
	{name: "_parent", reason: "no current Slack Audit kit consumer"},
	{name: "_session", reason: "no current Slack Audit kit consumer"},
}

type intrinsicDecision struct {
	name     string
	retained bool
	consumer string
	reason   string
}

func validateIntrinsicDecisions(decisions []intrinsicDecision) error {
	seen := make(map[string]struct{}, len(decisions))
	for _, decision := range decisions {
		if decision.name == "" {
			return errors.New("intrinsic decision has no name")
		}
		if _, duplicate := seen[decision.name]; duplicate {
			return errors.New("intrinsic decision is duplicated")
		}
		seen[decision.name] = struct{}{}
		if decision.retained && decision.consumer == "" {
			return errors.New("retained intrinsic has no current kit consumer")
		}
		if !decision.retained && decision.reason == "" {
			return errors.New("omitted intrinsic has no disposition")
		}
	}
	return nil
}

func TestCurrentKitIntrinsicContractOmitsEveryUnusedCandidate(t *testing.T) {
	want := []string{
		"_apiVersion", "_category", "_endpoint", "_parent", "_product",
		"_recordType", "_session", "_source", "_subcategory", "_vendor",
	}
	got := make([]string, 0, len(intrinsicDecisions))
	for _, decision := range intrinsicDecisions {
		got = append(got, decision.name)
		if decision.retained {
			t.Fatalf("unused intrinsic %q was retained for %q", decision.name, decision.consumer)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("candidate intrinsic inventory=%v want=%v", got, want)
	}
	if err := validateIntrinsicDecisions(intrinsicDecisions); err != nil {
		t.Fatal(err)
	}
}

// readPublicReadme returns the shipped operator documentation with its line
// wrapping collapsed, so a statement is pinned to its words rather than to where
// the paragraph happens to break.
func readPublicReadme(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(body)), " ")
}

// TestPublicReadmeDocumentsEveryPermanentSkipClass reads the shipped operator
// documentation and pins it to the skip behavior the plugin actually implements.
// A record in one of these classes is warned, receipted, and never delivered, so
// an operator cannot be left to discover a silent permanent drop.
func TestPublicReadmeDocumentsEveryPermanentSkipClass(t *testing.T) {
	readme := readPublicReadme(t)

	// Every permanent skip class in Plugin.Handle, with its exact limit.
	for name, statements := range map[string][]string{
		"raw record over the Slack raw-record limit": {
			fmt.Sprintf("%d MiB", maximumAuditRecordSize>>20),
			"raw Slack record larger than",
			"raw-record limit",
		},
		"normalization failure": {
			"fails normalization",
			"date_create",
		},
		"configured global ingest entry limit": {
			"configured global ingest entry-size limit",
			"Max-Entry-Size",
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, statement := range statements {
				if !strings.Contains(readme, statement) {
					t.Fatalf("README does not state %q for the %s skip class", statement, name)
				}
			}
		})
	}

	// The at-least-once boundary must be exact in both directions.
	for _, statement := range []string{
		"warned, receipted, and permanently skipped",
		"never delivered and is never retried",
		"delivered at least once and may be delivered more than once",
		"every record inside them is delivered zero times",
	} {
		if !strings.Contains(readme, statement) {
			t.Fatalf("README does not state the at-least-once boundary %q", statement)
		}
	}

	// A skip class named in the documentation but not implemented would be just
	// as wrong, so the stated count is pinned to the three implemented classes.
	if got, want := strings.Count(readme, "Three classes of record"), 1; got != want {
		t.Fatalf("README skip-class count statements=%d want=%d", got, want)
	}
}

// TestPublicReadmeDocumentsBoundedDeliveryAndPageReduction keeps the rest of the
// operator-visible delivery contract pinned to the implementation.
func TestPublicReadmeDocumentsBoundedDeliveryAndPageReduction(t *testing.T) {
	readme := readPublicReadme(t)
	for _, statement := range []string{
		"waits at most two minutes",
		fmt.Sprintf("%d MiB", maximumAuditPageSize>>20),
		"progressively fewer requested records",
		"never moves backwards",
	} {
		if !strings.Contains(readme, statement) {
			t.Fatalf("README does not state %q", statement)
		}
	}
	if acknowledgedWriteDeadline != 2*time.Minute {
		t.Fatalf("acknowledged write bound=%v but the README documents two minutes", acknowledgedWriteDeadline)
	}
}

func TestIntrinsicContractValidatorRejectsUnsupportedRetention(t *testing.T) {
	for name, decisions := range map[string][]intrinsicDecision{
		"retained without consumer": {{name: "_vendor", retained: true}},
		"omitted without reason":    {{name: "_vendor"}},
		"duplicate":                 {{name: "_vendor", reason: "unused"}, {name: "_vendor", reason: "unused"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateIntrinsicDecisions(decisions); err == nil {
				t.Fatal("invalid intrinsic contract accepted")
			}
		})
	}
}
