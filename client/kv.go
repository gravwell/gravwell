/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListKVTables returns all kv tables accessible to the current user.
func (c *Client) ListKVTables(opts types.QueryOptions) (ret types.KVTableListResponse, err error) {
	return c.post[types.QueryOptions, types.KVTableListResponse](KV_LIST_URL, &opts)
}

// ListAllKVTables (admin-only) returns all kv tables on the system.
func (c *Client) ListAllKVTables(opts types.QueryOptions) (ret types.KVTableListResponse, err error) {
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.KVTableListResponse](KV_LIST_URL, &opts)
}

// GetKVTable returns a particular kv table definition by ID.
func (c *Client) GetKVTable(id string) (types.KVTable, error) {
	return c.GetKVTableEx(id, GetOptions{})
}

// GetKVTableEx returns a particular kv table definition, modified by opts.
func (c *Client) GetKVTableEx(id string, opts GetOptions) (types.KVTable, error) {
	return c.get[types.KVTable](kvIdUrl(id), opts.params()...)
}

// DeleteKVTable soft-deletes a kv table. Its rows are kept until the table is purged.
func (c *Client) DeleteKVTable(id string) error {
	return c.delete(kvIdUrl(id), false)
}

// PurgeKVTable deletes a kv table entirely, dropping its rows.
func (c *Client) PurgeKVTable(id string) error {
	return c.delete(kvIdUrl(id), true)
}

// CreateKVTable creates a new kv table, returning the newly-created table.
func (c *Client) CreateKVTable(t types.KVTable) (result types.KVTable, err error) {
	return c.post[types.KVTable, types.KVTable](KV_URL, &t)
}

// UpdateKVTable modifies an existing kv table and returns the complete, updated struct.
// Setting p.Columns alters the table's schema.
func (c *Client) UpdateKVTable(ID string, p types.KVTablePatch) (updated types.KVTable, err error) {
	if ID == "" {
		return types.KVTable{}, ErrEmptyID
	}
	return c.patch[types.KVTablePatch, types.KVTable](kvIdUrl(ID), p)
}

// CleanupKVTables (admin-only) purges all deleted kv tables for all users.
func (c *Client) CleanupKVTables() error {
	return c.delete(KV_URL, false)
}

// GetKVTableRow returns one row of a kv table.
func (c *Client) GetKVTableRow(tableID, key string) (types.KVTableRow, error) {
	if err := checkKVRowArgs(tableID, key); err != nil {
		return types.KVTableRow{}, err
	}
	return c.get[types.KVTableRow](kvRowUrl(tableID, key))
}

// PutKVTableRow creates a row, or replaces it if the key already exists, and
// returns the row as written.
func (c *Client) PutKVTableRow(tableID, key string, w types.KVTableRowWrite) (types.KVTableRow, error) {
	if err := checkKVRowArgs(tableID, key); err != nil {
		return types.KVTableRow{}, err
	}
	return c.put[types.KVTableRowWrite, types.KVTableRow](kvRowUrl(tableID, key), &w)
}

// UpdateKVTableRow changes some of an existing row's columns and returns the updated row.
func (c *Client) UpdateKVTableRow(tableID, key string, p types.KVTableRowPatch) (types.KVTableRow, error) {
	if err := checkKVRowArgs(tableID, key); err != nil {
		return types.KVTableRow{}, err
	}
	return c.patch[types.KVTableRowPatch, types.KVTableRow](kvRowUrl(tableID, key), p)
}

// DeleteKVTableRow deletes one row.
func (c *Client) DeleteKVTableRow(tableID, key string) error {
	return c.deleteKVTableRow(tableID, key)
}

// DeleteKVTableRowVersion deletes one row, but only if its current Version
// equals expectedVersion.
func (c *Client) DeleteKVTableRowVersion(tableID, key string, expectedVersion int64) error {
	return c.deleteKVTableRow(tableID, key, urlParam{key: "expected_version", value: strconv.FormatInt(expectedVersion, 10)})
}

func (c *Client) deleteKVTableRow(tableID, key string, params ...urlParam) error {
	if err := checkKVRowArgs(tableID, key); err != nil {
		return err
	}
	resp, err := c.reqDriver(http.MethodDelete, kvRowUrl(tableID, key), nil, []int{http.StatusNoContent}, params...)
	defer drainResponse(resp)
	return err
}

// ReplaceKVTableRows atomically replaces every row in a kv table.
func (c *Client) ReplaceKVTableRows(tableID string, r types.KVTableRowsReplace) (types.KVTableRowsResult, error) {
	if tableID == "" {
		return types.KVTableRowsResult{}, ErrEmptyID
	}
	return c.put[types.KVTableRowsReplace, types.KVTableRowsResult](kvRowsUrl(tableID), &r)
}

// MergeKVTableRows atomically upserts and deletes many rows of a kv table.
func (c *Client) MergeKVTableRows(tableID string, m types.KVTableRowsMerge) (types.KVTableRowsResult, error) {
	if tableID == "" {
		return types.KVTableRowsResult{}, ErrEmptyID
	}
	return c.patch[types.KVTableRowsMerge, types.KVTableRowsResult](kvRowsUrl(tableID), m)
}

// ListKVTableRows returns a page of a kv table's rows. Filters and OrderBy in
// opts take column names.
func (c *Client) ListKVTableRows(tableID string, opts types.QueryOptions) (types.KVTableRowListResponse, error) {
	if tableID == "" {
		return types.KVTableRowListResponse{}, ErrEmptyID
	}
	opts.Filters = append(slices.Clone(opts.Filters), types.Filter{Key: "TableID", Operation: "=", Values: []any{tableID}})
	return c.post[types.QueryOptions, types.KVTableRowListResponse](KV_ROWS_LIST_URL, &opts)
}

func checkKVRowArgs(tableID, key string) error {
	if tableID == "" {
		return ErrEmptyID
	} else if key == "" {
		return ErrEmptyKey
	}
	return nil
}
