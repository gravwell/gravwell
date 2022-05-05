/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gravwell/gravwell/v3/ingest"
)

type regexListener struct {
	base
	Regex           string
	Tag_Name        string
	Cert_File       string
	Key_File        string
	Trim_Whitespace bool
	Max_Buffer      int // maximum number of bytes to buffer without finding a regular expression
	Preprocessor    []string
}

func (rl regexListener) Validate() error {
	if err := rl.base.Validate(); err != nil {
		return err
	}
	//process the default tag
	if _, err := rl.defaultTag(); err != nil {
		return err
	}
	// Make sure the regex compiles
	if _, err := regexp.Compile(rl.Regex); err != nil {
		return err
	}
	return nil
}

func (rl regexListener) defaultTag() (tag string, err error) {
	tag = strings.TrimSpace(rl.Tag_Name)
	if len(tag) == 0 {
		err = ErrMissingDefaultTag
	} else if err = ingest.CheckTag(tag); err != nil {
		err = fmt.Errorf("Invalid Tag-Name %v", err)
	}
	return
}
