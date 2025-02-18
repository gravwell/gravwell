/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"log"
	"runtime/debug"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	gravwelldebug "github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/turnage/graw/reddit"
	"github.com/turnage/graw/streams"
)

const ()

var (
	verboseFlag                   = flag.Bool("v", false, "Verbose output")
	ver                           = flag.Bool("version", false, "Print the version information and exit")
	expireIDs       time.Duration = 48 * time.Hour
	parentAuthors                 = map[string]commentToAuthor{}
	cleanThreshhold int           = 1000000

	verbose bool
)

type commentToAuthor struct {
	ID     string
	Author string
	TS     time.Time
}

func main() {
	go gravwelldebug.HandleDebugSignals("reddit")
	debug.SetTraceback("all")
	iw, err := NewIngestWriter()
	if err != nil {
		log.Println("Failed to create new ingest writer", err)
		return
	}

	dieChan := make(chan bool, 1)
	var wg sync.WaitGroup

	go func() {
		for {
			if err := redditConnection(iw, dieChan, &wg); err != nil {
				log.Println("Got error from reddit system: ", err)
				time.Sleep(10 * time.Second)
			}
		}
	}()

	//register quit signals so we can die gracefully
	utils.WaitForQuit()

	dieChan <- true
	wg.Wait()

	if err := iw.Close(); err != nil {
		log.Fatal("Failed to close ingest writer")
	}
}

func redditConnection(iw *ingestWriter, dieChan chan bool, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()
	apiHandle, err := reddit.NewScript(`gravwell reddit ingester`, time.Second)
	if err != nil {
		return err
	}
	killCh := make(chan bool, 1)
	errCh := make(chan error, 1)
	comments, err := streams.SubredditComments(apiHandle, killCh, errCh, subreddits...)
	if err != nil {
		return err
	}
	tck := time.NewTicker(10 * time.Second)
	defer tck.Stop()
mainLoop:
	for {
		select {
		case err, ok := <-errCh:
			if !ok {
				if verbose {
					log.Println("Reddit system error chan closed")
				}
			}
			killCh <- true
			return err
		case c, ok := <-comments:
			if !ok {
				if verbose {
					log.Println("Reddit system closed the comment channel")
				}
				break mainLoop
			}
			iw.emptyTimes = 0
			lc := TranslateCommentStructure(c)
			if _, ok := parentAuthors[c.ID]; ok {
				continue
			}
			ParentAuthorFunc(&lc)
			iw.AddComment(lc)
		case _ = <-tck.C:
			if err := iw.Flush(); err != nil {
				log.Println("Failed to flush comments", err)
			}
			cleanParentToAuthors()
			if iw.emptyTimes > 3 {
				// this means we've gone 30 seconds without reading any articles! bail out.
				if *verboseFlag {
					log.Println("Reddit system appears to have gone quiet, restarting")
				}
				break mainLoop
			}
		case _ = <-dieChan:
			if err := iw.Flush(); err != nil {
				log.Println("Failed to flush comments", err)
			}
			break mainLoop
		}
	}
	killCh <- true
	return nil
}

func ParentAuthorFunc(c *Comment) {
	if cta, ok := parentAuthors[c.ParentID]; ok {
		c.ParentAuthor = cta.Author
	}
	parentAuthors[c.ID] = commentToAuthor{
		ID:     c.ID,
		Author: c.Author,
		TS:     time.Now(),
	}
}

func cleanParentToAuthors() {
	if len(parentAuthors) < cleanThreshhold {
		return
	}
	expires := time.Now().Add(-1 * expireIDs)
	for k, v := range parentAuthors {
		if v.TS.Before(expires) {
			delete(parentAuthors, k)
		}
	}
}
