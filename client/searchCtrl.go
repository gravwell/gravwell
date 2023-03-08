/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v3/client/websocketRouter"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	SEARCH_HISTORY_USER = `user`
	importFormGID       = `GID`
	importFormFile      = `file`
	importFormBatchName = `BatchName`
	importFormBatchInfo = `BatchInfo`
)

var (
	ErrSearchNotAttached = errors.New("search not attached")
)

// DeleteSearch will request that a search is deleted by search ID
func (c *Client) DeleteSearch(sid string) error {
	return c.deleteStaticURL(searchCtrlIdUrl(sid), nil)
}

// SearchInfo requests the status for a given search ID
func (c *Client) SearchInfo(sid string) (types.SearchInfo, error) {
	var si types.SearchInfo
	if err := c.getStaticURL(searchCtrlDetailsUrl(sid), &si); err != nil {
		return si, err
	}
	return si, nil
}

// SaveSearch will request that a search is saved by ID
func (c *Client) SaveSearch(sid string) error {
	return c.patchStaticURL(searchCtrlSaveUrl(sid), nil)
}

// BackgroundSearch will request that a search is backgrounded by ID
func (c *Client) BackgroundSearch(sid string) error {
	return c.patchStaticURL(searchCtrlBackgroundUrl(sid), nil)
}

// SetGroup will set the GID of the group which can read the search.
// Setting it to 0 will disable group access.
func (c *Client) SetGroup(sid string, gid int32) error {
	request := struct{ GID int32 }{gid}
	return c.putStaticURL(searchCtrlGroupUrl(sid), request, nil)
}

// ListSearchStatuses returns a list of all searches the current user has access to
// and their current status.
func (c *Client) ListSearchStatuses() ([]types.SearchCtrlStatus, error) {
	var scs []types.SearchCtrlStatus
	if err := c.getStaticURL(SEARCH_CTRL_LIST_URL, &scs); err != nil {
		return nil, err
	}
	return scs, nil
}

// ListAllSearchStatuses returns a list of all searches on the system. Only admin
// users can use this function.
func (c *Client) ListAllSearchStatuses() ([]types.SearchCtrlStatus, error) {
	var scs []types.SearchCtrlStatus
	if err := c.getStaticURL(SEARCH_CTRL_LIST_ALL_URL, &scs); err != nil {
		return nil, err
	}
	return scs, nil
}

// ListSearchDetails returns details for all searches the current user has access to
// and their current status. If the admin flag is set (by calling SetAdminMode())
// this will return info for all searches on the system.
func (c *Client) ListSearchDetails() ([]types.SearchInfo, error) {
	var details []types.SearchInfo
	err := c.getStaticURL(searchCtrlListDetailsUrl(), &details)
	return details, err
}

// GetSearchHistory retrieves the current search history for the currently logged
// in user.  It only pulls back searches invoked by the individual user.
func (c *Client) GetSearchHistory() ([]types.SearchLog, error) {
	var sl []types.SearchLog
	if err := c.getStaticURL(searchHistoryUrl(SEARCH_HISTORY_USER, c.userDetails.UID), &sl); err != nil {
		return nil, err
	}
	return sl, nil
}

// GetRefinedSearchHistory retrieves the current search history for the
// currently logged in user narrowed to searches containing the substring s. It
// only pulls back searches invoked by the individual user.
func (c *Client) GetRefinedSearchHistory(s string) ([]types.SearchLog, error) {
	var sl []types.SearchLog
	params := map[string]string{
		"refine": s,
	}
	pth := searchHistoryUrl(SEARCH_HISTORY_USER, c.userDetails.UID)
	if err := c.methodStaticParamURL(http.MethodGet, pth, params, &sl); err != nil {
		return nil, err
	}
	return sl, nil
}

// GetUserSearchHistory retrieves the current search history for the specified user.
// Only admins may request search history for users besides themselves.
func (c *Client) GetUserSearchHistory(uid int32) ([]types.SearchLog, error) {
	var sl []types.SearchLog
	if err := c.getStaticURL(searchHistoryUrl(SEARCH_HISTORY_USER, uid), &sl); err != nil {
		return nil, err
	}
	return sl, nil
}

// GetSearchHistoryRange retrieves paginated search history for the currently logged
// in user.  The start and end parameters are indexes into the search history, with
// 0 representing the most recent search.
func (c *Client) GetSearchHistoryRange(start, end int) ([]types.SearchLog, error) {
	params := map[string]string{
		"start": fmt.Sprintf("%d", start),
		"end":   fmt.Sprintf("%d", end),
	}
	pth := searchHistoryUrl(SEARCH_HISTORY_USER, c.userDetails.UID)
	var sl []types.SearchLog
	if err := c.methodStaticParamURL(http.MethodGet, pth, params, &sl); err != nil {
		return nil, err
	}
	return sl, nil
}

// Search represents an search on the Gravwell system.
type Search struct {
	ID            string
	RenderMod     string
	searchSockets *SearchSockets
	searchOutput  *websocketRouter.SubProtoConn
	types.StartSearchRequest
}

func (s *Search) Exchange(req, resp interface{}) (err error) {
	if s.searchSockets == nil || s.searchOutput == nil {
		err = ErrSearchNotAttached
		return
	} else if req == nil {
		err = errors.New("invalid request")
		return
	} else if err = s.searchOutput.WriteJSON(req); err != nil {
		return
	}
	if resp != nil {
		err = s.searchOutput.ReadJSON(resp)
	}
	return
}

// Ping sends a message via the search's websockets (if present)
// to keep the sockets open. If you intend to run a search and then
// wait a long time before interacting with it further, you
// should periodically call Ping() to keep the connection alive.
func (s *Search) Ping() error {
	if s.searchSockets != nil && s.searchSockets.Pong != nil {
		var preq types.PingReq
		if err := s.searchSockets.Pong.WriteJSON(preq); err != nil {
			return err
		}
		if err := s.searchSockets.Pong.ReadJSON(&preq); err != nil {
			return err
		}
	} else {
		return ErrSearchNotAttached
	}
	return nil
}

// ParseSearch validates a search query. Gravwell will return an error if the query
// is not valid.
func (c *Client) ParseSearch(query string) (err error) {
	_, err = c.ParseSearchWithResponse(query, []types.FilterRequest{})
	return
}

// ParseSearchWithResponse behaves as ParseSearch, but it returns the ParseSearchResponse
// which contains detailed information about how Gravwell parsed out the search.
func (c *Client) ParseSearchWithResponse(query string, filters []types.FilterRequest) (psr types.ParseSearchResponse, err error) {
	ssr := types.ParseSearchRequest{
		SearchString: query,
		Sequence:     0x1337,
		Filters:      filters,
	}
	var s *SearchSockets
	if s, err = c.GetSearchSockets(); err != nil {
		return
	} else if err = s.Parse.WriteJSON(ssr); err != nil {
		return
	} else if err = s.Parse.ReadJSON(&psr); err != nil {
		return
	} else if err = closeSockets(s); err != nil {
		return
	}

	//check that what we got back was good
	if psr.ParseError != `` {
		err = fmt.Errorf("Parse error: %s", psr.ParseError)
	} else if ssr.Sequence != psr.Sequence {
		err = fmt.Errorf("Parse search response sequence is invalid: %d != %d",
			ssr.Sequence, psr.Sequence)
	} else if psr.RawQuery != query {
		err = fmt.Errorf("RawQuery response is invalid: %q != %q", psr.RawQuery, query)
	}
	return
}

// StartBackgroundSearch launches a backgrounded search with the given query
// and the specified start and end times. If "nohistory" is set, the search will
// be hidden in the user's search history; if false, it will be visible.
func (c *Client) StartBackgroundSearch(query string, start, end time.Time, nohistory bool) (s Search, err error) {
	sr := types.StartSearchRequest{
		SearchString: query,
		SearchStart:  start.Format(time.RFC3339Nano),
		SearchEnd:    end.Format(time.RFC3339Nano),
		NoHistory:    nohistory,
		Background:   true,
	}

	s, err = c.StartSearchEx(sr)
	return
}

// StartSearch launches a foregrounded search with the given query and start/end.
// If "nohistory" is set, the search will
// be hidden in the user's search history; if false, it will be visible.
func (c *Client) StartSearch(query string, start, end time.Time, nohistory bool) (s Search, err error) {
	sr := types.StartSearchRequest{
		SearchString: query,
		SearchStart:  start.Format(time.RFC3339Nano),
		SearchEnd:    end.Format(time.RFC3339Nano),
		NoHistory:    nohistory,
	}
	s, err = c.StartSearchEx(sr)
	return
}

// StartSearchExtended launches a search using a StartSearchRequest object
// This function grants the maximum amount of control over the search starting process
func (c *Client) StartSearchEx(sr types.StartSearchRequest) (s Search, err error) {
	//grab subprotocol connection and subproto parent
	s.searchSockets, err = c.GetSearchSockets()
	if err != nil {
		return
	}
	searchSubProto := s.searchSockets.Search
	// we get a new set of sockets on each GetSearchSocket.
	// So we can close the main search suproto when done
	// The main protocols for the search will be left open
	defer searchSubProto.Close()

	if err = searchSubProto.WriteJSON(sr); err != nil {
		return
	}

	ssresp := types.StartSearchResponse{}
	if err = searchSubProto.ReadJSON(&ssresp); err != nil {
		return
	}
	if ssresp.Error != "" {
		err = fmt.Errorf("Search request error: %s", ssresp.Error)
		return
	}

	if ssresp.OutputSearchSubproto != `` {
		if err = s.searchSockets.Client.AddSubProtocol(ssresp.OutputSearchSubproto); err != nil {
			return
		}
		if s.searchOutput, err = s.searchSockets.Client.GetSubProtoConn(ssresp.OutputSearchSubproto); err != nil {
			return
		}
	}

	sAck := types.StartSearchAck{
		Ok:                   true,
		OutputSearchSubproto: ssresp.OutputSearchSubproto,
	}
	if err = searchSubProto.WriteJSON(sAck); err != nil {
		err = fmt.Errorf("Failed to ack the search server: %w", err)
		return
	}

	s.ID = ssresp.SearchID
	s.RenderMod = ssresp.RenderModule
	s.StartSearchRequest = sr
	return
}

// StopSearch asks the search to stop progressing through the underlying data.
// The renderer maintains any data it currently has and the query is entirely usable,
// The data feed is just stopped.  Issuing a Stop command to a query that is done
// has no affect.  Meaning that if you attached to an archived search and issue a stop
// nothing happens.  Requests to stop queries that you don't own return an error
// unless the caller is an admin
func (c *Client) StopSearch(id string) (err error) {
	//send request
	err = c.putStaticURL(searchCtrlStopUrl(id), nil, nil)
	return
}

// StartFilteredSearch launches a foregrounded search with the given query and start/end.
// The filters parameter is an array of filters; these will be automatically inserted into the
// query during the parse phase.
// If "nohistory" is set, the search will
// be hidden in the user's search history; if false, it will be visible.
func (c *Client) StartFilteredSearch(query string, start, end time.Time, nohistory bool, filters []types.FilterRequest) (s Search, err error) {
	sr := types.StartSearchRequest{
		SearchString: query,
		SearchStart:  start.Format(time.RFC3339Nano),
		SearchEnd:    end.Format(time.RFC3339Nano),
		NoHistory:    nohistory,
		Filters:      filters,
	}

	s, err = c.StartSearchEx(sr)
	return
}

// AttachSearch connects to an existing search (specified with the id parameter) and
// returns the associated Search object.
func (c *Client) AttachSearch(id string) (s Search, err error) {
	//grab subprotocol connection and subproto parent
	s.ID = id
	s.searchSockets, err = c.GetAttachSockets()
	if err != nil {
		return
	}
	conn := s.searchSockets.Attach

	//attempt to attach to it
	req := types.AttachSearchRequest{
		ID: id,
	}
	var resp types.AttachSearchResponse
	if err := conn.WriteJSON(req); err != nil {
		return s, err
	}
	if err := conn.ReadJSON(&resp); err != nil {
		return s, err
	}
	if resp.Error != "" {
		return s, errors.New(resp.Error)
	}
	if len(resp.Subproto) == 0 || resp.Info == nil {
		return s, errors.New("Invalid search attach response")
	}

	//kick off our renderer
	if err := s.searchSockets.Client.AddSubProtocol(resp.Subproto); err != nil {
		return s, err
	}
	rconn, err := s.searchSockets.Client.GetSubProtoConn(resp.Subproto)
	if err != nil {
		return s, err
	}
	s.searchOutput = rconn
	s.RenderMod = resp.RendererMod
	return s, nil
}

// GetAvailableEntryCount returns the number of output entries for the specified
// search. The second return value is a boolean indicating if the search has finished
// or not.
func (c *Client) GetAvailableEntryCount(s Search) (uint64, bool, error) {
	//send request
	req := types.BaseRequest{
		ID: types.REQ_ENTRY_COUNT,
	}
	resp := types.BaseResponse{}
	if err := s.Exchange(req, &resp); err != nil {
		return 0, false, err
	} else if err = resp.Err(); err != nil {
		return 0, false, err
	} else if resp.ID != types.RESP_ENTRY_COUNT {
		return 0, false, errors.New("Invalid response ID")
	}
	return resp.EntryCount, resp.Finished, nil
}

// WaitForSearch sleeps until the given search is complete.
// If the search fails for some reason, WaitForSearch will return an error describing
// the reason for the failure.
func (c *Client) WaitForSearch(s Search) (err error) {
	var done bool
	for !done {
		if _, done, err = c.GetAvailableEntryCount(s); err != nil {
			return
		}
		time.Sleep(time.Second)
	}
	//how ask for the search details
	req := types.BaseRequest{
		ID: types.REQ_SEARCH_DETAILS,
	}
	var resp types.BaseResponse
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}

	return
}

// GetEntries fetches results from a search. These results have the Tag field represented
// as a string rather than the numeric representation used internally.
// Note that GetEntries is really only suitable for searches using the raw, text, or hex
// renderers. Results from the table renderer will also be restructured as entries, but
// other renderers are not supported.
func (c *Client) GetEntries(s Search, start, end uint64) ([]types.StringTagEntry, error) {
	if (end - start) < 0 {
		return nil, fmt.Errorf("invalid entry span: start = %v, end = %v", start, end)
	} else if (end - start) == 0 {
		return []types.StringTagEntry{}, nil
	}
	switch s.RenderMod {
	case types.RenderNamePcap:
		fallthrough
	case types.RenderNameRaw:
		fallthrough
	case types.RenderNameHex:
		fallthrough
	case types.RenderNameText:
		return c.getStringTagTextEntries(s, start, end)
	case types.RenderNameTable:
		return c.getStringTagTableEntries(s, start, end)
	}
	return nil, errors.New("Unsupported render module " + s.RenderMod)
}

func (c *Client) getStringTagTextEntries(s Search, start, end uint64) (ste []types.StringTagEntry, err error) {
	//send request
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	resp := types.TextResponse{}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != types.RESP_GET_ENTRIES {
		return nil, errors.New("Invalid response ID")
	} else if resp.Entries == nil {
		return nil, errors.New("Empty entry response")
	}

	// Build up a reverse map of tags
	tagMap := make(map[entry.EntryTag]string)
	for tagName, tagID := range resp.Tags {
		tagMap[tagID] = tagName
	}

	var ret []types.StringTagEntry
	for _, ent := range resp.Entries {
		ste := types.StringTagEntry{
			TS:         ent.TS.StandardTime(),
			SRC:        ent.SRC,
			Data:       ent.Data,
			Tag:        tagMap[ent.Tag],
			Enumerated: ent.Enumerated,
		}
		ret = append(ret, ste)
	}
	return ret, nil
}

func (c *Client) getStringTagTableEntries(s Search, start, end uint64) (ste []types.StringTagEntry, err error) {
	//send request
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	resp := types.TableResponse{}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}
	if resp.ID != types.RESP_GET_ENTRIES {
		err = errors.New("Invalid response ID")
		return
	}
	ste = []types.StringTagEntry{}

	columns := resp.Entries.Columns
	rows := resp.Entries.Rows
	if len(columns) == 0 || len(rows) == 0 {
		return
	}

	// Build up a reverse map of tags
	tagMap := make(map[entry.EntryTag]string)
	for tagName, tagID := range resp.Tags {
		tagMap[tagID] = tagName
	}

	for _, row := range rows {
		e := types.StringTagEntry{
			TS: row.TS.StandardTime(),
		}
		for i, v := range row.Row {
			if i >= len(columns) {
				continue
			}
			e.Enumerated = append(e.Enumerated, types.EnumeratedPair{
				Name:     columns[i],
				Value:    v,
				RawValue: types.RawEnumeratedValue{Type: 1, Data: []byte(v)},
			})
		}
		ste = append(ste, e)
	}
	return
}

func (c *Client) getTextResults(s Search, req types.TextRequest) (resp types.TextResponse, err error) {
	if s.RenderMod != types.RenderNameText && s.RenderMod != types.RenderNameHex && s.RenderMod != types.RenderNameRaw && s.RenderMod != types.RenderNamePcap {
		err = fmt.Errorf("Search %v has invalid renderer type %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetTextResults queries a range of search results from the text, hex, or raw renderers. It returns
// a types.TextResponse structure containing the results (see the Entries field)
func (c *Client) GetTextResults(s Search, first, last uint64) (types.TextResponse, error) {
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: first,
				Last:  last,
			},
		},
	}
	return c.getTextResults(s, req)
}

// GetTextTsRange queries search results for a time range from the text, hex, or raw
// renderers. It returns a types.TextResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetTextTsRange(s Search, start, end time.Time, first, last uint64) (types.TextResponse, error) {
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getTextResults(s, req)
}

// GetPcapResults queries a range of search results from the pcap renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
func (c *Client) GetPcapResults(s Search, start, end uint64) (types.TextResponse, error) {
	return c.GetTextResults(s, start, end)
}

// GetPcapTsRange queries search results for a time range from the pcap renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetPcapTsRange(s Search, start, end time.Time, first, last uint64) (types.TextResponse, error) {
	return c.GetTextTsRange(s, start, end, first, last)
}

// GetRawResults queries a range of search results from the raw renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
func (c *Client) GetRawResults(s Search, start, end uint64) (types.TextResponse, error) {
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_RAW_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getTextResults(s, req)
}

// GetRawTsRange queries search results for a time range from the raw renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetRawTsRange(s Search, start, end time.Time, first, last uint64) (types.TextResponse, error) {
	return c.GetTextTsRange(s, start, end, first, last)
}

// GetHexResults queries a range of search results from the hex renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field)
func (c *Client) GetHexResults(s Search, start, end uint64) (types.TextResponse, error) {
	return c.GetTextResults(s, start, end)
}

// GetHexTsRange queries search results for a time range from the hex renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetHexTsRange(s Search, start, end time.Time, first, last uint64) (types.TextResponse, error) {
	return c.GetTextTsRange(s, start, end, first, last)
}

func (c *Client) getTableResults(s Search, req types.TableRequest) (resp types.TableResponse, err error) {
	if s.RenderMod != types.RenderNameTable {
		err = fmt.Errorf("Search %v has invalid renderer type: expected table, saw %v", s.ID, s.RenderMod)
		return
	}

	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetTableResults queries a range of search results from the table renderer. It returns
// a types.TableResponse structure containing the results (see the Entries field)
func (c *Client) GetTableResults(s Search, start, end uint64) (types.TableResponse, error) {
	req := types.TableRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getTableResults(s, req)
}

// GetTableTsRange queries search results for a time range from the table
// renderer. It returns a types.TableResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetTableTsRange(s Search, start, end time.Time, first, last uint64) (types.TableResponse, error) {
	req := types.TableRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getTableResults(s, req)
}

func (c *Client) getGaugeResults(s Search, req types.TableRequest) (resp types.GaugeResponse, err error) {
	if s.RenderMod != types.RenderNameGauge && s.RenderMod != types.RenderNameNumbercard {
		err = fmt.Errorf("Search %v has invalid renderer type: expected gauge, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetGaugeResults queries a range of search results from the gauge or numbercard renderers.
// It returns a types.GaugeResponse structure containing the results (see the Entries field).
func (c *Client) GetGaugeResults(s Search, start, end uint64) (types.GaugeResponse, error) {
	req := types.TableRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getGaugeResults(s, req)
}

// GetGaugeTsRange queries search results for a time range from the gauge
// renderer. It returns a types.GaugeResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetGaugeTsRange(s Search, start, end time.Time, first, last uint64) (types.GaugeResponse, error) {
	req := types.TableRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getGaugeResults(s, req)
}

// GetNumbercardResults queries a range of search results from the gauge or numbercard renderers.
// It returns a types.GaugeResponse structure containing the results (see the Entries field).
func (c *Client) GetNumbercardResults(s Search, start, end uint64) (types.GaugeResponse, error) {
	return c.GetGaugeResults(s, start, end)
}

// GetNumbercardTsRange queries search results for a time range from the gauge or numbercard renderers.
// It returns a types.GaugeResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetNumbercardTsRange(s Search, start, end time.Time, first, last uint64) (types.GaugeResponse, error) {
	return c.GetGaugeTsRange(s, start, end, first, last)
}

func (c *Client) getChartResults(s Search, req types.ChartRequest) (resp types.ChartResponse, err error) {
	if s.RenderMod != types.RenderNameChart {
		err = fmt.Errorf("Search %v has invalid renderer type: expected chart, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetChartResults queries a range of search results from the chart renderer.
// It returns a types.ChartResponse structure containing the results (see the Entries field).
func (c *Client) GetChartResults(s Search, start, end uint64) (resp types.ChartResponse, err error) {
	req := types.ChartRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getChartResults(s, req)
}

// GetChartTsRange queries search results for a time range from the chart
// renderer. It returns a types.ChartResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetChartTsRange(s Search, start, end time.Time, first, last uint64) (types.ChartResponse, error) {
	req := types.ChartRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getChartResults(s, req)
}

func (c *Client) getFdgResults(s Search, req types.FdgRequest) (resp types.FdgResponse, err error) {
	if s.RenderMod != types.RenderNameFdg {
		err = fmt.Errorf("Search %v has invalid renderer type: expected fdg, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetFdgResults queries a range of search results from the FDG renderer.
// It returns a types.FdgResponse structure containing the results (see the Entries field).
func (c *Client) GetFdgResults(s Search, start, end uint64) (types.FdgResponse, error) {
	req := types.FdgRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getFdgResults(s, req)
}

// GetFdgTsRange queries search results for a time range from the fdg
// renderer. It returns a types.FdgResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetFdgTsRange(s Search, start, end time.Time, first, last uint64) (types.FdgResponse, error) {
	req := types.FdgRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getFdgResults(s, req)
}

func (c *Client) getStackGraphResults(s Search, req types.StackGraphRequest) (resp types.StackGraphResponse, err error) {
	if s.RenderMod != types.RenderNameStackGraph {
		err = fmt.Errorf("Search %v has invalid renderer type: expected stackgraph, saw %v", s.ID, s.RenderMod)
		return
	}

	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetStackGraphResults queries a range of search results from the stackgraph renderer.
// It returns a types.StackGraphResponse structure containing the results (see the Entries field).
func (c *Client) GetStackGraphResults(s Search, start, end uint64) (types.StackGraphResponse, error) {
	req := types.StackGraphRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	return c.getStackGraphResults(s, req)
}

// GetStackGraphTsRange queries search results for a time range from the stackgraph
// renderer. It returns a types.StackGraphResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetStackGraphTsRange(s Search, start, end time.Time, first, last uint64) (types.StackGraphResponse, error) {
	req := types.StackGraphRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
	}
	return c.getStackGraphResults(s, req)
}

func (c *Client) getPointmapResults(s Search, req types.PointmapRequest) (resp types.PointmapResponse, err error) {
	if s.RenderMod != types.RenderNamePointmap {
		err = fmt.Errorf("Search %v has invalid renderer type: expected pointmap, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetPointmapResults queries a range of search results from the pointmap renderer.
// It returns a types.PointmapResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetPointmapResults(s Search, start, end uint64, fence types.Geofence) (types.PointmapResponse, error) {
	req := types.PointmapRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
		Fence: fence,
	}
	return c.getPointmapResults(s, req)
}

// GetPointmapTsRange queries search results for a time range from the pointmap
// renderer. It returns a types.PointmapResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetPointmapTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.PointmapResponse, error) {
	req := types.PointmapRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
		Fence: fence,
	}
	return c.getPointmapResults(s, req)
}

func (c *Client) getHeatmapResults(s Search, req types.HeatmapRequest) (resp types.HeatmapResponse, err error) {
	if s.RenderMod != types.RenderNameHeatmap {
		err = fmt.Errorf("Search %v has invalid renderer type: expected heatmap, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetHeatmapResults queries a range of search results from the heatmap renderer.
// It returns a types.HeatmapResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetHeatmapResults(s Search, start, end uint64, fence types.Geofence) (types.HeatmapResponse, error) {
	req := types.HeatmapRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
		Fence: fence,
	}
	return c.getHeatmapResults(s, req)
}

// GetHeatmapTsRange queries search results for a time range from the heatmap
// renderer. It returns a types.HeatmapResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetHeatmapTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.HeatmapResponse, error) {
	req := types.HeatmapRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
		Fence: fence,
	}
	return c.getHeatmapResults(s, req)
}

func (c *Client) getP2PResults(s Search, req types.P2PRequest) (resp types.P2PResponse, err error) {
	if s.RenderMod != types.RenderNameP2P {
		err = fmt.Errorf("Search %v has invalid renderer type: expected point2point, saw %v", s.ID, s.RenderMod)
		return
	}
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != req.ID {
		err = errors.New("Invalid response ID")
	}
	return
}

// GetP2PResults queries a range of search results from the point2point renderer.
// It returns a types.P2PResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetP2PResults(s Search, start, end uint64, fence types.Geofence) (types.P2PResponse, error) {
	req := types.P2PRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
		Fence: fence,
	}
	return c.getP2PResults(s, req)
}

// GetP2PTsRange queries search results for a time range from the point2point
// renderer. It returns a types.P2PResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetP2PTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.P2PResponse, error) {
	req := types.P2PRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_TS_RANGE,
			EntryRange: &types.EntryRange{
				StartTS: entry.FromStandard(start),
				EndTS:   entry.FromStandard(end),
				First:   first,
				Last:    last,
			},
		},
		Fence: fence,
	}
	return c.getP2PResults(s, req)
}

// GetExploreEntries takes the same arguments as GetEntries (a search + start and
// end indices), but in addition to the array of SearchEntries, it returns an
// array of ExploreResult objects. Each ExploreResult corresponds to the SearchEntry
// at the same index.
func (c *Client) GetExploreEntries(s Search, start, end uint64) ([]types.SearchEntry, []types.ExploreResult, error) {
	if (end - start) < 0 {
		return nil, nil, fmt.Errorf("invalid entry span: start = %v, end = %v", start, end)
	} else if (end - start) == 0 {
		return []types.SearchEntry{}, []types.ExploreResult{}, nil
	}
	//send request
	req := types.TextRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_GET_EXPLORE_ENTRIES,
			EntryRange: &types.EntryRange{
				First: start,
				Last:  end,
			},
		},
	}
	resp := types.TextResponse{}
	if err := s.Exchange(req, &resp); err != nil {
		return nil, nil, err
	} else if err = resp.Err(); err != nil {
		return nil, nil, err
	} else if resp.ID != types.RESP_GET_EXPLORE_ENTRIES {
		return nil, nil, errors.New("Invalid response ID")
	}
	//see what the server has to say about that
	if resp.Entries == nil {
		return nil, nil, errors.New("Empty entry response")
	}
	return resp.Entries, resp.Explore, nil
}

// GetSearchMetadata request the enumerated value metadata stats from a search.
// The metadata stats contain some basic survey info about enumerated values in the pipeline.
// The survey info may contain numerical info such as min and max for numbers and a sample
// of enumerated value values for non-numerical types.
func (c *Client) GetSearchMetadata(s Search) (sm types.SearchMetadata, err error) {
	req := types.StatsRequest{
		BaseRequest: types.BaseRequest{
			ID: types.REQ_SEARCH_METADATA,
		},
	}
	var resp types.StatsResponse
	if err = s.Exchange(req, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	} else if resp.ID != types.RESP_SEARCH_METADATA {
		err = errors.New("Invalid response ID")
	}
	if resp.Metadata != nil {
		sm = *resp.Metadata
	} else {
		err = errors.New("Empty metadata")
	}
	return
}

func closeSockets(s *SearchSockets) (err error) {
	if s != nil {
		if s.Parse != nil {
			if lerr := s.Parse.Close(); lerr != nil {
				if err == nil {
					err = lerr
				}
			}
		}
		if s.Search != nil {
			if lerr := s.Search.Close(); lerr != nil {
				if err == nil {
					err = lerr
				}
			}
		}
		if s.Attach != nil {
			if lerr := s.Attach.Close(); lerr != nil {
				if err == nil {
					err = lerr
				}
			}
		}
		if s.Pong != nil {
			if lerr := s.Pong.Close(); lerr != nil {
				if err == nil {
					err = lerr
				}
			}
		}
		if s.Client != nil {
			if lerr := s.Client.Close(); lerr != nil {
				if err == nil {
					err = lerr
				}
			}
		}
	}
	return
}

// DetachSearch disconnects the client from a search. This may lead to the search being
// garbage collected.
func (c *Client) DetachSearch(s Search) {
	if s.searchOutput == nil {
		return
	}
	req := types.BaseRequest{
		ID: types.REQ_CLOSE,
	}
	s.searchOutput.WriteJSON(req)

	//attempt to send the close command
	closeSockets(s.searchSockets)
	if s.searchOutput != nil {
		s.searchOutput.Close()
	}
}

// DownloadSearch returns an io.ReadCloser which can be used to download the results of the search
// with the specified search ID. The tr parameter is the time frame over which to download
// results, and the format parameter specifies the desired download format
// ("json", "csv", "text", "pcap", "lookupdata", "ipexist", "archive")
func (c *Client) DownloadSearch(sid string, tr types.TimeRange, format string) (r io.ReadCloser, err error) {
	var resp *http.Response
	if resp, err = c.SearchDownloadRequest(sid, format, tr); err != nil {
		return
	} else if resp.StatusCode != 200 {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		err = fmt.Errorf("Bad response %d", resp.StatusCode)
	} else {
		r = resp.Body
	}
	return
}

// ImportSearch uploads an archived search to Gravwell. The gid parameter specifies
// a group to share with, if desired.
func (c *Client) ImportSearch(rdr io.Reader, gid int32) (err error) {
	var flds map[string]string
	if gid > 0 {
		if !c.userDetails.InGroup(gid) {
			err = fmt.Errorf("Logged in user not in group %d", gid)
			return
		}
		flds = map[string]string{
			importFormGID: strconv.FormatInt(int64(gid), 10),
		}
	}
	return c.importSearch(rdr, flds)
}

// ImportSearchBatchInfo uploads an archived search to Gravwell with optional batch information.
// The gid parameter specifies a group to share with, if desired.
// The name and info parameters are optional extended batch information
func (c *Client) ImportSearchBatchInfo(rdr io.Reader, gid int32, name, info string) (err error) {
	flds := map[string]string{}
	if gid > 0 {
		if !c.userDetails.InGroup(gid) {
			err = fmt.Errorf("Logged in user not in group %d", gid)
			return
		}
		flds[importFormGID] = strconv.FormatInt(int64(gid), 10)
	}
	if name != `` {
		flds[importFormBatchName] = name
	}
	if info != `` {
		flds[importFormBatchInfo] = info
	}

	return c.importSearch(rdr, flds)
}

func (c *Client) importSearch(rdr io.Reader, flds map[string]string) (err error) {
	var resp *http.Response
	if resp, err = c.uploadMultipartFile(searchCtrlImportUrl(), importFormFile, `file`, rdr, flds); err != nil {
		return
	}
	if resp.StatusCode != 200 {
		if err = decodeBodyError(resp.Body); err != nil {
			err = fmt.Errorf("response error status %d %v", resp.StatusCode, err)
		} else {
			err = fmt.Errorf("Invalid response %s(%d)", resp.Status, resp.StatusCode)
		}
	}
	resp.Body.Close()
	return
}
