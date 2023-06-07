/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/gravwell/gravwell/v3/client"
	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/ingest"
)

type shardRange struct {
	start time.Time
	end   time.Time
	size  uint64
}

type wellSet struct {
	name   string
	tags   []string
	shards []shardRange
}

func resolveWellSets(cli *client.Client, mp map[string]types.IndexerWellData) (wss []wellSet, err error) {
	wells := make(map[string][]string)
	for _, v := range mp {
		for _, well := range v.Wells {
			w, ok := wells[well.Name]
			if !ok {
				w = well.Tags
			} else {
				w = consolidateTags(w, well.Tags)
			}
			wells[well.Name] = w
		}
	}

	//get a well map rolling to make lookups faster
	wellMap := make(map[string]wellSet, len(wells))
	for k, v := range wells {
		wellMap[k] = wellSet{
			name: k,
			tags: v,
		}

	}

	// now iterate over the shards and add them in
	for _, v := range mp {
		for _, well := range v.Wells {
			if wm, ok := wellMap[well.Name]; ok {
				for _, si := range well.Shards {
					wm.shards = append(wm.shards, shardRange{
						start: si.Start,
						end:   si.End,
						size:  si.Size,
					})
				}
				wellMap[well.Name] = wm
			}
		}
	}
	wss = make([]wellSet, 0, len(wellMap))
	for _, v := range wellMap {
		wss = append(wss, wellSet{
			name:   v.name,
			tags:   v.tags,
			shards: consolidateShards(v.shards),
		})
	}

	return
}

func consolidateShards(shards []shardRange) (r []shardRange) {
	existing := map[int64]uint64{}
	for _, v := range shards {
		if !cutoff.IsZero() {
			if v.end.Before(cutoff) {
				continue //shard is completely out of range
			} else if v.start.Before(cutoff) {
				v.start = cutoff //shard is partially out of range, update it
			}
		}
		sz, ok := existing[v.start.Unix()]
		if !ok {
			sz = v.size
			r = append(r, v)
		} else {
			sz += v.size
		}
		existing[v.start.Unix()] = sz
	}
	//get things sorted
	sort.SliceStable(r, func(i, j int) bool {
		return r[i].start.Before(r[j].start)
	})

	for i, ws := range r {
		r[i].size = existing[ws.start.Unix()]
	}
	return
}

func consolidateTags(orig, incoming []string) (r []string) {
	r = orig
	for _, v := range incoming {
		if !inSet(v, r) {
			r = append(r, v)
		}
	}
	return
}

func inSet(r string, set []string) bool {
	for _, v := range set {
		if r == v {
			return true
		}
	}
	return false
}

func outputTotals(bytes uint64) {
	fmt.Printf("\rTotal Data Processed: %s                                     ",
		ingest.HumanSize(bytes))
}
