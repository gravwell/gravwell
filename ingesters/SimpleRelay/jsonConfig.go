/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	elemSep string = `:`

	defaultMaxObjectSize uint = 1024 * 1024 //1MB is huge
)

var (
	ErrMissingDefaultTag     = errors.New("Missing default tag")
	ErrMissingJSONTagMatches = errors.New("Missing JSON Tag matches")
	ErrEmptyJSONFields       = errors.New("Missing JSON field match")
)

type jsonListener struct {
	baseConfig
	Max_Object_Size uint
	Disable_Compact bool
	Extractor       string
	Default_Tag     string
	Tag_Match       []string
}

func (jl *jsonListener) Validate() error {
	if err := jl.baseConfig.Validate(); err != nil {
		return err
	}
	jl.initDefaultTag() //make sure we resolve the Tag-Name and Default-Tag configs to do the right thing
	//process the default tag
	if _, err := jl.defaultTag(); err != nil {
		return err
	}

	//check if the listener is doing some extractions and tag routing
	if len(jl.Tag_Match) > 0 || len(jl.Extractor) > 0 {
		//check the match translators
		if _, err := jl.TagMatchers(); err != nil {
			return err
		}

		//check the extraction element
		if _, err := jl.GetJsonFields(); err != nil {
			return err
		}
	}

	//check the max object size
	if jl.Max_Object_Size == 0 {
		jl.Max_Object_Size = defaultMaxObjectSize
	}
	return nil
}

func (jl *jsonListener) initDefaultTag() {
	if strings.TrimSpace(jl.Default_Tag) == `` {
		if v := strings.TrimSpace(jl.Tag_Name); v != `` {
			jl.Default_Tag = v
		} else {
			jl.Default_Tag = entry.DefaultTagName
		}
	}
}

func (jl jsonListener) defaultTag() (tag string, err error) {
	tag = strings.TrimSpace(jl.Default_Tag)
	if len(tag) == 0 {
		err = ErrMissingDefaultTag
	} else if err = ingest.CheckTag(tag); err != nil {
		err = fmt.Errorf("Invalid Default-Tag %v", err)
	}
	return
}

type TagMatcher struct {
	Value string
	Tag   string
}

func (jl jsonListener) TagMatchers() (tags []TagMatcher, err error) {
	if len(jl.Tag_Match) == 0 && len(jl.Extractor) == 0 {
		return
	}
	var tm TagMatcher
	if len(jl.Tag_Match) == 0 {
		err = ErrMissingJSONTagMatches
	} else if len(jl.Extractor) == 0 {
		err = ErrEmptyJSONFields
	} else {
		//process each of the tag matches
		for i := range jl.Tag_Match {
			if tm.Value, tm.Tag, err = extractElementTag(jl.Tag_Match[i]); err != nil {
				break
			}
			tags = append(tags, tm)
		}
	}
	return
}

func (jl jsonListener) Tags() (tags []string, err error) {
	var tms []TagMatcher
	tags = []string{jl.Default_Tag}
	if tms, err = jl.TagMatchers(); err != nil || len(tms) == 0 {
		return
	}
	mp := map[string]bool{
		jl.Default_Tag: true,
	}
	for _, tm := range tms {
		mp[tm.Tag] = true
	}
	for k, _ := range mp {
		tags = append(tags, k)
	}

	return
}

func extractElementTag(v string) (match, tag string, err error) {
	var flds []string
	s := bufio.NewScanner(strings.NewReader(v))
	s.Buffer(make([]byte, initDataSize), maxDataSize)
	s.Split(colonSplitter)
	for s.Scan() {
		if len(s.Text()) == 0 {
			continue
		}
		flds = append(flds, s.Text())
	}
	if len(flds) < 2 {
		err = fmt.Errorf("Invalid Tag-Match element.  Missing match and tag.")
	} else if len(flds) > 2 {
		err = fmt.Errorf("Invalid Tag-Match element.  Too many elements")
	}
	if err == nil {
		match = flds[0]
		tag = strings.TrimSpace(flds[1])
		err = ingest.CheckTag(tag)
	}
	return
}

func (jl jsonListener) GetJsonFields() (flds []string, err error) {
	if len(jl.Tag_Match) == 0 && len(jl.Extractor) == 0 {
		return // no error, no fields
	}
	return getJsonFields(jl.Extractor)
}

func getJsonFields(v string) (flds []string, err error) {
	s := bufio.NewScanner(strings.NewReader(v))
	s.Buffer(make([]byte, initDataSize), maxDataSize)
	s.Split(dotSplitter)
	for s.Scan() {
		if len(s.Text()) == 0 {
			continue
		}
		flds = append(flds, s.Text())
	}
	if len(flds) == 0 {
		err = ErrEmptyJSONFields
	}

	return
}

func checkJsonConfigs(lsts map[string]*jsonListener) error {
	for k, v := range lsts {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("JSONListener %q configuration is invalid: %w", k, err)
		}
	}
	return nil
}

func isSpace(r rune) bool {
	if r > '\u00ff' {
		return false
	}

	// only support ASCII for now
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	case '\u0085', '\u00A0':
		return true
	}
	return false
}

func dotSplitter(data []byte, atEOF bool) (int, []byte, error) {
	return tokenSplitter(data, atEOF, '.')
}

func colonSplitter(data []byte, atEOF bool) (int, []byte, error) {
	return tokenSplitter(data, atEOF, ':')
}

func tokenSplitter(data []byte, atEOF bool, item rune) (int, []byte, error) {
	var openQuote bool
	var escaped bool
	// Skip leading spaces.
	start := 0
	for width := 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if !isSpace(r) { //split on words and commas
			break
		}
	}
	// Scan until we get a single '|', marking end of module.
	for width, i := 0, start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if r == '\\' {
			escaped = true
			continue
		}
		//if we see an open quote, keep going until it closes
		if r == '"' && !escaped {
			openQuote = !openQuote
		}
		escaped = false
		if openQuote {
			continue
		}
		if r == item {
			return i + width, trimToken(data[start:i]), nil
		}
	}
	// If we're at EOF, we have a final, non-empty, non-terminated word. Return it.
	if atEOF && len(data) > start {
		return len(data), trimToken(data[start:]), nil
	}
	// Request more data.
	return start, nil, nil
}

func trimToken(s []byte) []byte {
	s = bytes.TrimSpace(s)
	if len(s) > 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}
