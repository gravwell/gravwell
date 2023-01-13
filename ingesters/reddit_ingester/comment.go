/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"strings"

	"github.com/turnage/graw/reddit"
)

type Comment struct {
	ID           string
	Name         string
	Permalink    string
	CreatedUTC   uint64
	Deleted      bool
	Author       string
	LinkAuthor   string
	LinkURL      string
	LinkTitle    string
	Subreddit    string
	Body         string
	ParentID     string
	ParentAuthor string
}

func TranslateCommentStructure(c *reddit.Comment) Comment {
	var pid string
	bits := strings.Split(c.ParentID, "_")
	if len(bits) > 1 {
		pid = bits[1]
	} else {
		pid = c.ParentID
	}
	return Comment{
		ID:         c.ID,
		Name:       c.Name,
		Permalink:  c.Permalink,
		CreatedUTC: c.CreatedUTC,
		Deleted:    c.Deleted,
		Author:     c.Author,
		LinkAuthor: c.LinkAuthor,
		LinkURL:    c.LinkURL,
		LinkTitle:  c.LinkTitle,
		Subreddit:  c.Subreddit,
		Body:       c.Body,
		ParentID:   pid,
		//ParentAuthor:
	}
}
