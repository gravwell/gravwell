package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const maxChunkSize = 256 * 1024 * 1024 //256MB at a time

var (
	totalProcessed uint64
)

func processWell(cli *client.Client, base, well string, tags []string, shards []shardRange) (err error) {
	pth := filepath.Join(base, well)
	if err = os.MkdirAll(pth, 0700); err != nil {
		return
	}
	fmt.Printf("processing well %s to %s containing %v tags and %v shards\n",
		well, pth, len(tags), len(shards))
	query := fmt.Sprintf(`tag=%s nosort | raw`, strings.Join(tags, ","))

	for _, shard := range shards {
		if err = processShard(cli, pth, query, shard.start, shard.end, shard.size); err != nil {
			err = fmt.Errorf("Failed to process data on well %s [%v - %v] - %v",
				well, shard.start, shard.end, err)
			break
		}
	}
	fmt.Printf("\nDONE\n")
	return
}

func processShard(cli *client.Client, pth, query string, start, end time.Time, rangeSize uint64) (err error) {
	dur := (end.Sub(start).Truncate(time.Second) + time.Second)
	chunkDur := resolveChunkDuration(dur, rangeSize)
	for s := start; s.Before(end); s = s.Add(chunkDur) {
		var chunkSize int64
		e := s.Add(chunkDur)
		if e.After(end) {
			e = end
		}
		if chunkSize, err = processChunk(cli, s, e, pth, query); err != nil {
			err = fmt.Errorf("failed to process chunk at %v - %w", s, err)
			return
		}
		totalProcessed += uint64(chunkSize)
		outputTotals(totalProcessed)
	}
	return
}

func processChunk(cli *client.Client, s, e time.Time, pth, query string) (sz int64, err error) {
	var search client.Search
	var fout *os.File
	var rdr io.ReadCloser
	fpath := filepath.Join(pth, s.Format("2006-01-02-15:04:05.json.gz"))

	//check if we have already exported this chunk
	if _, err = os.Stat(fpath); err == nil {
		return
	} else if !os.IsNotExist(err) {
		return
	}
	err = nil
	ssr := types.StartSearchRequest{
		NoHistory:    true,
		NonTemporal:  true,
		SearchString: query,
		SearchStart:  s.Format(time.RFC3339),
		SearchEnd:    e.Format(time.RFC3339),
	}
	if search, err = cli.StartSearchEx(ssr); err != nil {
		return
	}
	defer cli.DetachSearch(search)
	if fout, err = os.Create(fpath); err != nil {
		err = fmt.Errorf("Failed to create output file %w", err)
		return
	}
	defer fout.Close()
	wtr := gzip.NewWriter(fout)
	tr := types.TimeRange{
		StartTS: entry.FromStandard(s),
		EndTS:   entry.FromStandard(e),
	}
	if rdr, err = cli.DownloadSearch(search.ID, tr, `json`); err != nil {
		err = fmt.Errorf("Failed to download data %w", err)
		return
	}
	sz, _ = io.Copy(wtr, rdr)
	rdr.Close()
	if err = wtr.Close(); err != nil {
		return
	}
	if sz == 0 {
		//empty shard, delete the output file
		err = os.Remove(fpath)
	}
	return
}

func resolveChunkDuration(dur time.Duration, totalSize uint64) (rdur time.Duration) {
	if totalSize <= maxChunkSize {
		return dur
	}
	chunks := (totalSize / maxChunkSize) + 1
	if chunks <= 1 {
		// just cut it in half for safety
		rdur = (dur / 2).Truncate(time.Second) + time.Second
	} else {
		rdur = (dur / time.Duration(chunks)).Truncate(time.Second) + time.Second
	}
	return
}
