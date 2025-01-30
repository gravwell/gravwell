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

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	SEARCH_HISTORY_USER = `user`
	importFormGID       = `GID`
	importFormFile      = `file`
	importFormBatchName = `BatchName`
	importFormBatchInfo = `BatchInfo`

	defaultInterval = 2 * time.Second
)

var (
	ErrSearchNotAttached       = errors.New("search not attached")
	ErrInvalidDateRequestRange = errors.New("Table request range is invalid")
)

// DeleteSearch will request that a search is deleted by search ID
func (c *Client) DeleteSearch(sid string) error {
	return c.deleteStaticURL(searchCtrlIdUrl(sid), nil)
}

// SearchStatus requests the status of a given search ID
func (c *Client) SearchStatus(sid string) (types.SearchCtrlStatus, error) {
	var si types.SearchCtrlStatus
	if err := c.getStaticURL(searchCtrlIdUrl(sid), &si); err != nil {
		return si, err
	}
	return si, nil
}

// SearchInfo requests the search info for a given search ID
func (c *Client) SearchInfo(sid string) (types.SearchInfo, error) {
	var si types.SearchInfo
	if err := c.getStaticURL(searchCtrlDetailsUrl(sid), &si); err != nil {
		return si, err
	}
	return si, nil
}

// SaveSearch will request that a search is saved by ID, an optional SaveSearchPatch can be sent
// to modify the expiration or search name and notes
func (c *Client) SaveSearch(sid string, ssp ...types.SaveSearchPatch) error {
	var arg interface{}
	if len(ssp) == 1 {
		arg = ssp[0]
	}
	return c.patchStaticURL(searchCtrlSaveUrl(sid), arg)
}

// BackgroundSearch will request that a search is backgrounded by ID
func (c *Client) BackgroundSearch(sid string) error {
	return c.patchStaticURL(searchCtrlBackgroundUrl(sid), nil)
}

// SetGroup will set the GID of the group which can read the search.
// Setting it to 0 will disable group access.
// Deprecated: use SetGroups instead
func (c *Client) SetGroup(sid string, gid int32) error {
	request := struct{ GID int32 }{gid}
	return c.putStaticURL(searchCtrlGroupUrl(sid), request)
}

// SetGroups sets the list of groups that can read the search
func (c *Client) SetGroups(sid string, gids []int32) error {
	request := struct{ GIDs []int32 }{gids}
	return c.putStaticURL(searchCtrlGroupsUrl(sid), request)
}

// SetGlobal is an admin-only function to toggle sharing of results
// with the entire system.
func (c *Client) SetGlobal(sid string, global bool) error {
	request := struct{ Global bool }{global}
	return c.putStaticURL(searchCtrlGlobalUrl(sid), request)
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
	params := []urlParam{
		urlParam{key: `refine`, value: s},
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
	params := []urlParam{
		urlParam{key: `start`, value: fmt.Sprintf("%d", start)},
		urlParam{key: `end`, value: fmt.Sprintf("%d", end)},
	}
	pth := searchHistoryUrl(SEARCH_HISTORY_USER, c.userDetails.UID)
	var sl []types.SearchLog
	if err := c.methodStaticParamURL(http.MethodGet, pth, params, &sl); err != nil {
		return nil, err
	}
	return sl, nil
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
		Filters:      filters,
	}
	if err = c.postStaticURL(searchParseUrl(), ssr, &psr); err != nil {
		return
	}

	//check that what we got back was good
	if psr.ParseError != `` {
		err = fmt.Errorf("Parse error: %s", psr.ParseError)
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
		SearchStart:  start.Format(time.RFC3339),
		SearchEnd:    end.Format(time.RFC3339),
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
	var resp types.LaunchResponse
	if err = c.postStaticURL(searchLaunchUrl(), sr, &resp); err != nil {
		return
	}
	//populate the time range in the search object from the search, we use what the server says, not what we handed in
	s.start, s.end = resp.Info.StartRange, resp.Info.EndRange

	s.ID = resp.SearchID
	s.RenderMod = resp.RenderModule
	if s.interval = time.Duration(resp.RefreshInterval) * time.Second; s.interval == 0 {
		s.interval = defaultInterval
	}
	s.session = resp.SearchSessionID
	s.cli = c
	s.SearchInfo = resp.Info
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
	err = c.putStaticURL(searchCtrlStopUrl(id), nil)
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
		SearchStart:  start.Format(time.RFC3339),
		SearchEnd:    end.Format(time.RFC3339),
		NoHistory:    nohistory,
		Filters:      filters,
	}

	s, err = c.StartSearchEx(sr)
	return
}

// AttachSearch connects to an existing search (specified with the id parameter) and
// returns the associated Search object.
func (c *Client) AttachSearch(id string) (s Search, err error) {
	var resp types.LaunchResponse
	if err = c.getStaticURL(searchAttachUrl(id), &resp); err != nil {
		return
	}
	//populate the time range in the search object from the search, we use what the server says, not what we handed in
	s.start, s.end = resp.Info.StartRange, resp.Info.EndRange

	s.ID = resp.SearchID
	s.RenderMod = resp.RenderModule
	if s.interval = time.Duration(resp.RefreshInterval) * time.Second; s.interval == 0 {
		s.interval = defaultInterval
	}
	s.session = resp.SearchSessionID
	s.cli = c
	s.SearchInfo = resp.Info

	return s, nil
}

// GetAvailableEntryCount returns the number of output entries for the specified
// search. The second return value is a boolean indicating if the search has finished
// or not.
func (c *Client) GetAvailableEntryCount(s Search) (uint64, bool, error) {
	st, err := c.GetSearchOverviewStats(s, 1, time.Time{}, time.Time{})
	if err != nil {
		return 0, false, err
	}
	return st.EntryCount, st.Finished, nil
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
	return nil
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

func (c *Client) getStringTagTextEntries(s Search, first, last uint64) (ste []types.StringTagEntry, err error) {
	var resp types.RawResponse
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	}
	if err = resp.Err(); err != nil {
		return
	}

	// Build up a reverse map of tags
	tagMap := make(map[entry.EntryTag]string)
	for tagName, tagID := range resp.Tags {
		tagMap[tagID] = tagName
	}

	if len(resp.Entries) == 0 {
		//nothing to convert, short circuit out
		return
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

func (c *Client) getStringTagTableEntries(s Search, first, last uint64) (ste []types.StringTagEntry, err error) {
	var resp types.TableResponse
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	}
	if err = resp.Err(); err != nil {
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

func (c *Client) getRenderResults(s Search, er types.EntryRange, obj interface{}) (err error) {
	params := []urlParam{
		s.sidParam(),
		ezParam(`first`, er.First),
		ezParam(`last`, er.Last),
		ezParam(`start`, er.StartTS.Format(time.RFC3339)),
		ezParam(`end`, er.EndTS.Format(time.RFC3339)),
	}
	err = c.getStaticURL(searchEntriesUrl(s.ID, s.RenderMod), obj, params...)
	return
}

func (c *Client) getFencedRenderResults(s Search, er types.EntryRange, fence types.Geofence, obj interface{}) (err error) {
	if fence.Enabled() == false {
		err = c.getRenderResults(s, er, obj)
		return
	}

	params := []urlParam{
		s.sidParam(),
		ezParam(`first`, er.First),
		ezParam(`last`, er.Last),
		ezParam(`start`, er.StartTS.Format(time.RFC3339)),
		ezParam(`end`, er.EndTS.Format(time.RFC3339)),
		ezParam(`swlat`, fence.SouthWest.Lat),
		ezParam(`swlong`, fence.SouthWest.Long),
		ezParam(`nelat`, fence.NorthEast.Lat),
		ezParam(`nelong`, fence.NorthEast.Long),
	}
	err = c.getStaticURL(searchEntriesUrl(s.ID, s.RenderMod), obj, params...)
	return
}

func checkRender(s Search, v string) (err error) {
	if s.RenderMod != v {
		//add in special case for guage, numbercard, text, hex, and raw
		switch v {
		case types.RenderNameGauge:
			if s.RenderMod == types.RenderNameNumbercard {
				return //this is ok
			}
		case types.RenderNameNumbercard:
			if s.RenderMod == types.RenderNameGauge {
				return //this is ok
			}
		case types.RenderNameHex:
			if s.RenderMod == types.RenderNameText {
				return //this is ok
			}
		case types.RenderNameText:
			fallthrough
		case types.RenderNamePcap:
			if s.RenderMod == types.RenderNameRaw {
				return //this is ok
			}
		}
		err = fmt.Errorf("Search %v has invalid renderer type %v not %v", s.ID, s.RenderMod, v)
	}
	return
}

func (c *Client) getTextResults(s Search, first, last uint64, start, end time.Time) (resp types.TextResponse, err error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	if err = checkRender(s, types.RenderNameText); err != nil {
		return
	} else if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}
	return
}

// GetTextResults queries a range of search results from the text, hex, or raw renderers. It returns
// a types.TextResponse structure containing the results (see the Entries field)
func (c *Client) GetTextResults(s Search, first, last uint64) (types.TextResponse, error) {
	return c.getTextResults(s, first, last, s.start, s.end)
}

// GetTextTsRange queries search results for a time range from the text, hex, or raw
// renderers. It returns a types.TextResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetTextTsRange(s Search, start, end time.Time, first, last uint64) (types.TextResponse, error) {
	return c.getTextResults(s, first, last, start, end)
}

func (c *Client) getPcapResults(s Search, er types.EntryRange) (resp types.RawResponse, err error) {
	if err = checkRender(s, types.RenderNamePcap); err != nil {
		return
	} else if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}
	return
}

// GetPcapResults queries a range of search results from the pcap renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
func (c *Client) GetPcapResults(s Search, first, last uint64) (types.RawResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getPcapResults(s, er)
}

// GetPcapTsRange queries search results for a time range from the pcap renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetPcapTsRange(s Search, start, end time.Time, first, last uint64) (types.RawResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getPcapResults(s, er)
}

func (c *Client) getRawResults(s Search, first, last uint64, start, end time.Time) (resp types.RawResponse, err error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	if err = checkRender(s, types.RenderNameRaw); err != nil {
		return
	} else if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}
	return
}

// GetRawResults queries a range of search results from the raw renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
func (c *Client) GetRawResults(s Search, first, last uint64) (types.RawResponse, error) {
	return c.getRawResults(s, first, last, s.start, s.end)
}

// GetRawTsRange queries search results for a time range from the raw renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetRawTsRange(s Search, start, end time.Time, first, last uint64) (resp types.RawResponse, err error) {
	resp, err = c.getRawResults(s, first, last, start, end)
	return
}

// GetHexResults queries a range of search results from the hex renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field)
func (c *Client) GetHexResults(s Search, first, last uint64) (resp types.TextResponse, err error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	resp, err = c.getHexResults(s, er)
	return
}

func (c *Client) getHexResults(s Search, er types.EntryRange) (resp types.TextResponse, err error) {
	if err = checkRender(s, types.RenderNameHex); err != nil {
		return
	} else if err = c.getRenderResults(s, er, &resp); err != nil {
		return
	} else if err = resp.Err(); err != nil {
		return
	}
	return
}

// GetHexTsRange queries search results for a time range from the hex renderer. It returns
// a types.TextResponse structure containing the results (see the Entries field).
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan specified.
func (c *Client) GetHexTsRange(s Search, start, end time.Time, first, last uint64) (resp types.TextResponse, err error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	resp, err = c.getHexResults(s, er)
	return
}

func (c *Client) getTableResults(s Search, er types.EntryRange) (resp types.TableResponse, err error) {
	if err = checkRender(s, types.RenderNameTable); err == nil {
		if err = c.getRenderResults(s, er, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetTableResults queries a range of search results from the table renderer. It returns
// a types.TableResponse structure containing the results (see the Entries field)
func (c *Client) GetTableResults(s Search, start, end uint64) (types.TableResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getTableResults(s, er)
}

// GetTableTsRange queries search results for a time range from the table
// renderer. It returns a types.TableResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetTableTsRange(s Search, start, end time.Time, first, last uint64) (types.TableResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getTableResults(s, er)
}

func (c *Client) getGaugeResults(s Search, er types.EntryRange) (resp types.GaugeResponse, err error) {
	if err = checkRender(s, types.RenderNameGauge); err == nil {
		if err = c.getRenderResults(s, er, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetGaugeResults queries a range of search results from the gauge or numbercard renderers.
// It returns a types.GaugeResponse structure containing the results (see the Entries field).
func (c *Client) GetGaugeResults(s Search, start, end uint64) (types.GaugeResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getGaugeResults(s, er)
}

// GetGaugeTsRange queries search results for a time range from the gauge
// renderer. It returns a types.GaugeResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetGaugeTsRange(s Search, start, end time.Time, first, last uint64) (types.GaugeResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getGaugeResults(s, er)
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

func (c *Client) getChartResults(s Search, er types.EntryRange) (resp types.ChartResponse, err error) {
	if err = checkRender(s, types.RenderNameChart); err == nil {
		if err = c.getRenderResults(s, er, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetChartResults queries a range of search results from the chart renderer.
// It returns a types.ChartResponse structure containing the results (see the Entries field).
func (c *Client) GetChartResults(s Search, start, end uint64) (resp types.ChartResponse, err error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getChartResults(s, er)
}

// GetChartTsRange queries search results for a time range from the chart
// renderer. It returns a types.ChartResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetChartTsRange(s Search, start, end time.Time, first, last uint64) (types.ChartResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getChartResults(s, er)
}

func (c *Client) getFdgResults(s Search, er types.EntryRange) (resp types.FdgResponse, err error) {
	if err = checkRender(s, types.RenderNameFdg); err == nil {
		if err = c.getRenderResults(s, er, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetFdgResults queries a range of search results from the FDG renderer.
// It returns a types.FdgResponse structure containing the results (see the Entries field).
func (c *Client) GetFdgResults(s Search, start, end uint64) (types.FdgResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getFdgResults(s, er)
}

// GetFdgTsRange queries search results for a time range from the fdg
// renderer. It returns a types.FdgResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetFdgTsRange(s Search, start, end time.Time, first, last uint64) (types.FdgResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getFdgResults(s, er)
}

func (c *Client) getStackGraphResults(s Search, er types.EntryRange) (resp types.StackGraphResponse, err error) {
	if err = checkRender(s, types.RenderNameStackGraph); err == nil {
		if err = c.getRenderResults(s, er, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetStackGraphResults queries a range of search results from the stackgraph renderer.
// It returns a types.StackGraphResponse structure containing the results (see the Entries field).
func (c *Client) GetStackGraphResults(s Search, start, end uint64) (types.StackGraphResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getStackGraphResults(s, er)
}

// GetStackGraphTsRange queries search results for a time range from the stackgraph
// renderer. It returns a types.StackGraphResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
func (c *Client) GetStackGraphTsRange(s Search, start, end time.Time, first, last uint64) (types.StackGraphResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getStackGraphResults(s, er)
}

func (c *Client) getPointmapResults(s Search, er types.EntryRange, fence types.Geofence) (resp types.PointmapResponse, err error) {
	if err = checkRender(s, types.RenderNamePointmap); err == nil {
		if err = c.getFencedRenderResults(s, er, fence, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetPointmapResults queries a range of search results from the pointmap renderer.
// It returns a types.PointmapResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetPointmapResults(s Search, start, end uint64, fence types.Geofence) (types.PointmapResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getPointmapResults(s, er, fence)
}

// GetPointmapTsRange queries search results for a time range from the pointmap
// renderer. It returns a types.PointmapResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetPointmapTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.PointmapResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getPointmapResults(s, er, fence)
}

func (c *Client) getHeatmapResults(s Search, er types.EntryRange, fence types.Geofence) (resp types.HeatmapResponse, err error) {
	if err = checkRender(s, types.RenderNameHeatmap); err == nil {
		if err = c.getFencedRenderResults(s, er, fence, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetHeatmapResults queries a range of search results from the heatmap renderer.
// It returns a types.HeatmapResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetHeatmapResults(s Search, first, last uint64, fence types.Geofence) (types.HeatmapResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getHeatmapResults(s, er, fence)
}

// GetHeatmapTsRange queries search results for a time range from the heatmap
// renderer. It returns a types.HeatmapResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetHeatmapTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.HeatmapResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getHeatmapResults(s, er, fence)
}

func (c *Client) getP2PResults(s Search, er types.EntryRange, fence types.Geofence) (resp types.P2PResponse, err error) {
	if err = checkRender(s, types.RenderNameP2P); err == nil {
		if err = c.getFencedRenderResults(s, er, fence, &resp); err == nil {
			err = resp.Err()
		}
	}
	return
}

// GetP2PResults queries a range of search results from the point2point renderer.
// It returns a types.P2PResponse structure containing the results (see the Entries field).
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetP2PResults(s Search, start, end uint64, fence types.Geofence) (types.P2PResponse, error) {
	er := types.EntryRange{
		First:   start,
		Last:    end,
		StartTS: entry.FromStandard(s.start),
		EndTS:   entry.FromStandard(s.end),
	}
	return c.getP2PResults(s, er, fence)
}

// GetP2PTsRange queries search results for a time range from the point2point
// renderer. It returns a types.P2PResponse structure containing the results (see the Entries field)
// The 'first' and 'last' parameters specify indexes of entries to fetch within the timespan
// specified.
// The fence parameter is an option geofence to apply to the results.
func (c *Client) GetP2PTsRange(s Search, start, end time.Time, first, last uint64, fence types.Geofence) (types.P2PResponse, error) {
	er := types.EntryRange{
		First:   first,
		Last:    last,
		StartTS: entry.FromStandard(start),
		EndTS:   entry.FromStandard(end),
	}
	return c.getP2PResults(s, er, fence)
}

// GetExploreEntries takes the same arguments as GetEntries (a search + start and
// end indices), but in addition to the array of SearchEntries, it returns an
// array of ExploreResult objects. Each ExploreResult corresponds to the SearchEntry
// at the same index.
func (c *Client) GetExploreEntries(s Search, start, end uint64) ([]types.SearchEntry, []types.ExploreResult, error) {
	var resp types.RawResponse
	if (end - start) < 0 {
		return nil, nil, fmt.Errorf("invalid entry span: start = %v, end = %v", start, end)
	} else if (end - start) == 0 {
		return []types.SearchEntry{}, []types.ExploreResult{}, nil
	}

	params := []urlParam{
		s.sidParam(),
		ezParam(`first`, start),
		ezParam(`last`, end),
		ezParam(`start`, s.StartRange.Format(time.RFC3339)),
		ezParam(`end`, s.EndRange.Format(time.RFC3339)),
	}
	if err := c.getStaticURL(searchExploreUrl(s.ID, s.RenderMod), &resp, params...); err != nil {
		return nil, nil, err
	} else if err = resp.Err(); err != nil {
		return nil, nil, err
	}
	return resp.Entries, resp.Explore, nil
}

// GetSearchMetadata request the enumerated value metadata stats from a search.
// The metadata stats contain some basic survey info about enumerated values in the pipeline.
// The survey info may contain numerical info such as min and max for numbers and a sample
// of enumerated value values for non-numerical types.
func (c *Client) GetSearchMetadata(s Search) (sm types.SearchMetadata, err error) {
	err = c.getStaticURL(searchStatsMetadataUrl(s.ID), &sm, s.sidParam())
	return
}

func (c *Client) getStats(s Search, count uint, start, end time.Time, pth string, obj interface{}) (err error) {
	if count == 0 {
		err = errors.New("invalid count")
		return
	}
	if start.IsZero() {
		start = s.start
	}
	if end.IsZero() {
		end = s.end
	}
	params := []urlParam{
		s.sidParam(),
		urlParam{key: `count`, value: fmt.Sprintf("%d", count)},
		urlParam{key: `start`, value: start.Format(time.RFC3339)},
		urlParam{key: `end`, value: end.Format(time.RFC3339)},
	}
	err = c.getStaticURL(pth, obj, params...)
	return
}

// GetSearchStatsOverview returns a set of overview stats for the query
func (c *Client) GetSearchOverviewStats(s Search, count uint, start, end time.Time) (sm types.OverviewStats, err error) {
	err = c.getStats(s, count, start, end, searchStatsOverviewUrl(s.ID), &sm)
	return
}

// GetSearchStats returns a set of overview stats for the query
func (c *Client) GetSearchStats(s Search, count uint, start, end time.Time) (ss []types.StatSet, err error) {
	err = c.getStats(s, count, start, end, searchStatsUrl(s.ID), &ss)
	return
}

// DetachSearch disconnects the client from a search. This may lead to the search being garbage collected.
func (c *Client) DetachSearch(s Search) {
	c.putStaticURL(searchDetachUrl(s.ID), nil, s.sidParam())
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

// Search represents an search on the Gravwell system.
type Search struct {
	ID        string
	RenderMod string
	start     time.Time //start range of the query
	end       time.Time //end range of query
	interval  time.Duration
	session   uuid.UUID
	cli       *Client

	types.SearchInfo
}

// Ping sends a message via the search's websockets (if present)
// to keep the sockets open. If you intend to run a search and then
// wait a long time before interacting with it further, you
// should periodically call Ping() to keep the connection alive.
func (s *Search) Ping() error {
	return s.ping(0)
}

// Close will close our handle on the search, effectively releasing our lock.
// The search will be cleaned up if there are no other clients and it is not a backgrounded/saved search.
func (s *Search) Close() error {
	return s.cli.putStaticURL(searchDetachUrl(s.ID), nil, s.sidParam())
}

func (s *Search) sidParam() (p urlParam) {
	p.key = urlSidParamKey
	p.value = s.session.String()
	return
}

func (s *Search) ping(iu uint) error {
	if s == nil || s.cli == nil {
		//uuuuh... bye...
		return ErrSearchNotAttached
	}
	var resp types.SearchSessionIntervalUpdate
	req := types.SearchSessionIntervalUpdate{
		Interval: iu,
	}
	params := []urlParam{s.sidParam()}
	if err := s.cli.methodStaticPushURL(http.MethodPut, searchPingUrl(s.ID), req, &resp, nil, params); err != nil {
		return err
	}
	if resp.Interval > 0 {
		s.interval = time.Duration(resp.Interval) * time.Second
	}
	return nil
}

// Interval is the duration that the webserver has asked us to update on
// basically a "check back in this often please" to keep the search session alive
func (s *Search) Interval() time.Duration {
	if s != nil {
		return s.interval
	}
	return 0
}

// UpdateInterval asks the webserver to change the required update interval,
// updating the interval is useful when we know we are going to wait a while
// and we don't want to have to provide proof of life really often.
func (s *Search) UpdateInterval(d time.Duration) error {
	if d < time.Second {
		return errors.New("intervals must be > 1s")
	}
	d = d.Round(time.Second)
	iu := uint(d / time.Second)
	return s.ping(iu)
}
