package slack

import (
	"errors"
	"slices"
	"testing"
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
