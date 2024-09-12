/*************************************************************************
* Copyright 2019 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package tags

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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

type TagResolver interface {
	NegotiateTag(string) (entry.EntryTag, error)
}

type TaggerConfig struct {
	Tags []string
}

func (tc TaggerConfig) Validate() (err error) {
	_, _, err = tc.TagSet()
	return
}

func (tc TaggerConfig) TagSet() (tags []string, globs []glob.Glob, err error) {
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

type Tagger struct {
	TaggerConfig
	tagmap map[string]entry.EntryTag
	globs  []glob.Glob
	tr     TagResolver
	tm     TagMask
}

func NewTagger(tc TaggerConfig, tr TagResolver) (*Tagger, error) {
	var globs []glob.Glob
	var tgs []string
	var err error
	var mask TagMask
	var tg entry.EntryTag
	tagMap := make(map[string]entry.EntryTag, len(tgs))
	if tr == nil {
		return nil, errors.New("tag config resolver is nil")
	}
	if tgs, globs, err = tc.TagSet(); err != nil {
		return nil, err
	}

	//swing through and negotiate our tags
	for _, tn := range tgs {
		if err := ingest.CheckTag(tn); err != nil {
			return nil, err
		} else if tg, err = tr.NegotiateTag(tn); err != nil {
			return nil, fmt.Errorf("Failed to negotiate tag %s: %w", tn, err)
		}
		tagMap[tn] = tg
		mask.Set(tg)
	}

	return &Tagger{
		TaggerConfig: tc,
		tagmap:       tagMap,
		globs:        globs,
		tr:           tr,
		tm:           mask,
	}, nil
}

func (t *Tagger) AllowedName(tn string) bool {
	ok, err := t.allowedTag(tn)
	if err != nil {
		ok = false
	}
	return ok
}

func (t *Tagger) allowedTag(tn string) (ok bool, err error) {
	if len(tn) == 0 {
		err = errors.New("Empty tag name")
		return
	} else if err = ingest.CheckTag(tn); err != nil {
		return
	} else if _, ok = t.tagmap[tn]; ok {
		return
	}
	//check the globs
	for i := range t.globs {
		if t.globs[i].Match(tn) {
			ok = true
			break
		}
	}
	return
}

func (t *Tagger) Negotiate(tn string) (tg entry.EntryTag, err error) {
	var ok bool
	if tg, ok = t.tagmap[tn]; ok {
		return
	}
	//tag hasn't been negotiated yet
	if tg, err = t.tr.NegotiateTag(tn); err == nil {
		if ok, err = t.allowedTag(tn); err == nil && ok {
			t.tm.Set(tg) //set the bitmask
		}
	}
	return
}

func (t *Tagger) Allowed(tg entry.EntryTag) bool {
	return t.tm.IsSet(tg)
}

type TagMask [1024]uint64

func (tm *TagMask) Set(tg entry.EntryTag) {
	idx, mask := tm.tagPosition(tg)
	tm[idx] |= mask
}

func (tm *TagMask) Clear(tg entry.EntryTag) {
	idx, mask := tm.tagPosition(tg)
	tm[idx] ^= mask
}

func (tm *TagMask) IsSet(tg entry.EntryTag) bool {
	idx, mask := tm.tagPosition(tg)
	return (tm[idx] & mask) == mask
}

func (tm *TagMask) tagPosition(tg entry.EntryTag) (idx int, mask uint64) {
	idx = int(tg) / 64
	mask = 1 << (uint64(tg) % 64)
	return
}
