package mother

import (
	"reflect"
	"testing"
)

func Test_quoteSplitTokens(t *testing.T) {
	tests := []struct {
		name               string
		oldTokens          []string
		wantStrippedTokens []string
	}{
		{"no alterations",
			[]string{
				"--flag1", "value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
			},
			[]string{
				"--flag1", "value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
			},
		},
		{"mixed style",
			[]string{
				"--flag1=value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
				"-b=value4",
				"argValue3",
			},
			[]string{
				"--flag1", "value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
				"-b", "value4",
				"argValue3",
			},
		},
		{"mixed style with boolean flags",
			[]string{
				"--boolFlag1",
				"--flag1=value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
				"-n",
				"-b=value4",
				"argValue3",
			},
			[]string{
				"--boolFlag1",
				"--flag1", "value1",
				"--flag2", "value2",
				"argValue",
				"-a", "value3",
				"argValue2",
				"-n",
				"-b", "value4",
				"argValue3",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotStrippedTokens := quoteSplitTokens(tt.oldTokens); !reflect.DeepEqual(gotStrippedTokens, tt.wantStrippedTokens) {
				t.Errorf("quoteSplitTokens() = %v, want %v", gotStrippedTokens, tt.wantStrippedTokens)
			}
		})
	}

	t.Run("no tokens", func(t *testing.T) {
		got := quoteSplitTokens([]string{})
		if len(got) != 0 {
			t.Errorf("quoteSplitTokens() = %v (len: %v), want [] (len: 0)", got, len(got))

		}
	})
}
