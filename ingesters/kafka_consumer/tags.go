/*************************************************************************
* Copyright 2019 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	wildCards string = `?*[]{}!-`
)

var (
	ErrNoTags           = errors.New("No tags specified")
	ErrEmptyTag         = errors.New("Empty tag")
	ErrMaxTagsAllocated = errors.New("Maximum number of tags allocated")
	ErrDisallowedTag    = errors.New("Tag not allowed")
	ErrUnknownTag       = errors.New("Unknown tag")
)

type tagResolver interface {
	NegotiateTag(string) (entry.EntryTag, error)
}

type taggerConfig struct {
	Tags []string
}

func (tc taggerConfig) validate() (err error) {
	_, _, err = tc.TagSet()
	return
}

func (tc taggerConfig) TagSet() (tags []string, globs []glob.Glob, err error) {
	if len(tc.Tags) == 0 {
		err = ErrNoTags
		return
	}
	// Build up two lists: regular tags, and wildcard globs
	for _, tn := range tc.Tags {
		if bytes.ContainsAny([]byte(tn), wildCards) {
			//treat it as a wildcard
			var g glob.Glob
			if g, err = glob.Compile(tn); err != nil {
				return
			}
			globs = append(globs, g)
		} else if err = ingest.CheckTag(tn); err != nil {
			return
		} else {
			tags = append(tags, tn)
		}
	}
	return
}

func (tc taggerConfig) initTagSet(defTag string, tr tagResolver) (tm map[string]entry.EntryTag, globs []glob.Glob, err error) {
	var tgs []string
	var tg entry.EntryTag
	if tr == nil {
		err = errors.New("tag config resolver is nil")
		return
	} else if defTag == `` {
		err = errors.New("default tag is missing")
		return
	} else if err = ingest.CheckTag(defTag); err != nil {
		return
	} else if tgs, globs, err = tc.TagSet(); err != nil {
		return
	}
	tm = make(map[string]entry.EntryTag, len(tgs)+1)

	//swing through and negotiate our tags
	if tg, err = tr.NegotiateTag(defTag); err != nil {
		err = fmt.Errorf("Failed to negotiate default tag %s: %v", defTag, err)
		return
	} else {
		tm[defTag] = tg
	}

	for _, tn := range tgs {
		if tg, err = tr.NegotiateTag(tn); err != nil {
			err = fmt.Errorf("Failed to negotiate tag %s: %v", tn, err)
			return
		} else {
			tm[tn] = tg
		}
	}

	return
}

type tagger struct {
	taggerConfig
	tagmap map[string]entry.EntryTag
	globs  []glob.Glob
}

func newTagger(tc taggerConfig, defTag string, tr tagResolver) (*tagger, error) {
	tm, globs, err := tc.initTagSet(defTag, tr)
	if err != nil {
		return nil, err
	}
	return &tagger{
		taggerConfig: tc,
		tagmap:       tm,
		globs:        globs,
	}, nil
}

func (t *tagger) allowed(tn string) bool {
	if len(tn) == 0 {
		return false
	} else if err := ingest.CheckTag(tn); err != nil {
		return false
	}
	//check the globs
	for i := range t.globs {
		if t.globs[i].Match(tn) {
			return true
		}
	}
	return false
}
