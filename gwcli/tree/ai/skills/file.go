/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gravwell/gravwell/v4/client/types"
)

// A skill file is a markdown document. It may open with a front matter block
// naming the skill and describing it:
//
//	---
//	name: query-skills
//	description: How to write Gravwell queries.
//	---
//	# Query skills
//	...
//
// Only name and description are read from the front matter; the block is not
// part of the skill's body. A file without one is named after itself.

const frontMatterFence = "---"

var errNotUTF8 = errors.New("the file is not valid UTF-8")

// skillFile is a skill as read from disk.
type skillFile struct {
	Name        string
	Description string
	Body        string
	// FromFrontMatter reports whether Name/Description came from the file's
	// front matter rather than a default, so replace knows what to patch.
	FromFrontMatter bool
}

// skillFromFile reads a skill file. The name falls back to the file's base
// name without its extension.
func skillFromFile(path string) (skillFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return skillFile{}, err
	}
	sf, err := parseSkill(string(b))
	if err != nil {
		return skillFile{}, err
	}
	if sf.Name == "" {
		base := filepath.Base(path)
		sf.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return sf, nil
}

// parseSkill splits a skill document into its front matter and body.
func parseSkill(doc string) (skillFile, error) {
	if !utf8.ValidString(doc) {
		return skillFile{}, errNotUTF8
	}
	doc = strings.TrimPrefix(doc, "\uFEFF") // a BOM is not content
	var sf skillFile
	lines := strings.SplitAfter(doc, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != frontMatterFence {
		sf.Body = doc
		return sf, nil
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if line == frontMatterFence {
			sf.Body = strings.Join(lines[i+1:], "")
			sf.FromFrontMatter = true
			return sf, nil
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			if strings.TrimSpace(line) == "" {
				continue
			}
			return skillFile{}, fmt.Errorf("front matter line %d is not a key: value pair: %q", i+1, line)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			sf.Name = value
		case "description":
			sf.Description = value
		}
	}
	return skillFile{}, errors.New("front matter is not closed with a --- line")
}

// render writes a skill back out in the file format, front matter first, so
// a downloaded skill uploads unchanged.
func render(s types.AgentSkill) string {
	var sb strings.Builder
	sb.WriteString(frontMatterFence + "\n")
	sb.WriteString("name: " + s.Name + "\n")
	if s.Description != "" {
		sb.WriteString("description: " + s.Description + "\n")
	}
	sb.WriteString(frontMatterFence + "\n")
	sb.WriteString(s.Body)
	if !strings.HasSuffix(s.Body, "\n") {
		sb.WriteString("\n")
	}
	return sb.String()
}
