/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"testing"
)

type ignoreTestConfig struct {
	prefixes []string
	globs    []string
	inputs   []ignoreInput
}

type ignoreInput struct {
	input      string
	shouldDrop bool
}

var ignoreTests = []ignoreTestConfig{
	{
		prefixes: []string{"foo", "bar"},
		globs:    nil,
		inputs: []ignoreInput{
			{
				input:      "foo blah",
				shouldDrop: true,
			},
			{
				input:      "not foo",
				shouldDrop: false,
			},
		},
	},
	{
		prefixes: []string{"foo", "bar"},
		globs:    []string{"*foo"},
		inputs: []ignoreInput{
			{
				input:      "foo blah",
				shouldDrop: true,
			},
			{
				input:      "not foo",
				shouldDrop: true,
			},
		},
	},
}

func TestIgnore(t *testing.T) {
	for _, test := range ignoreTests {
		li, err := NewIgnorer(test.prefixes, test.globs)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range test.inputs {
			if li.Ignore([]byte(input.input)) != input.shouldDrop {
				t.Fatal("incorrect drop")
			}
		}
	}
}
