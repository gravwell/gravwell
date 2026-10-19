/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"slices"
	"strconv"

	"github.com/gravwell/gravwell/v4/client/queryparams"
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListDataTables returns all tables accessible to the current user.
func (c *Client) ListDataTables(opts types.QueryOptions) (ret types.DataTableListResponse, err error) {
	return c.post[types.QueryOptions, types.DataTableListResponse](LIST_TABLES_URL, &opts)
}

// ListAllDataTables (admin-only) returns all tables on the system.
func (c *Client) ListAllDataTables(opts types.QueryOptions) (ret types.DataTableListResponse, err error) {
	opts.All = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.DataTableListResponse](LIST_TABLES_URL, &opts)
}

// GetDataTable returns a particular table definition by ID.
func (c *Client) GetDataTable(id string) (types.DataTable, error) {
	return c.GetDataTableEx(id, GetOptions{})
}

// GetDataTableEx returns a particular table definition, modified by opts.
func (c *Client) GetDataTableEx(id string, opts GetOptions) (types.DataTable, error) {
	return c.get[types.DataTable](tablesIdUrl(id), opts.params()...)
}

// DeleteDataTable soft-deletes a table. Its rows are kept until the table is purged.
func (c *Client) DeleteDataTable(id string) error {
	return c.delete(tablesIdUrl(id))
}

// PurgeDataTable deletes a table entirely, dropping its rows.
func (c *Client) PurgeDataTable(id string) error {
	return c.delete(tablesIdUrl(id), DeleteOptions{Purge: true}.params()...)
}

// CreateDataTable creates a new table, returning the newly-created table.
func (c *Client) CreateDataTable(t types.DataTable) (result types.DataTable, err error) {
	return c.post[types.DataTable, types.DataTable](TABLES_URL, &t)
}

// UpdateDataTable modifies an existing table and returns the complete, updated struct.
// Setting p.Columns alters the table's schema.
func (c *Client) UpdateDataTable(ID string, p types.DataTablePatch) (updated types.DataTable, err error) {
	if ID == "" {
		return types.DataTable{}, ErrEmptyID
	}
	return c.patch[types.DataTablePatch, types.DataTable](tablesIdUrl(ID), p)
}

// CleanupDataTables (admin-only) purges all deleted tables for all users.
func (c *Client) CleanupDataTables() error {
	return c.delete(TABLES_URL)
}

// GetDataTableRow returns one row of a table. Here and in the other row
// methods, key is the row's value in the table's key column, in text form.
func (c *Client) GetDataTableRow(tableID, key string) (types.DataTableRow, error) {
	if err := checkDataTableRowArgs(tableID, key); err != nil {
		return types.DataTableRow{}, err
	}
	return c.get[types.DataTableRow](tablesIdRowsKeyUrl(tableID, key))
}

// PutDataTableRow creates a row, or replaces it if the key already exists, and
// returns the row as written.
func (c *Client) PutDataTableRow(tableID, key string, w types.DataTableRowWrite) (types.DataTableRow, error) {
	if err := checkDataTableRowArgs(tableID, key); err != nil {
		return types.DataTableRow{}, err
	}
	return c.put[types.DataTableRowWrite, types.DataTableRow](tablesIdRowsKeyUrl(tableID, key), &w)
}

// UpdateDataTableRow changes some of an existing row's columns and returns the updated row.
func (c *Client) UpdateDataTableRow(tableID, key string, p types.DataTableRowPatch) (types.DataTableRow, error) {
	if err := checkDataTableRowArgs(tableID, key); err != nil {
		return types.DataTableRow{}, err
	}
	return c.patch[types.DataTableRowPatch, types.DataTableRow](tablesIdRowsKeyUrl(tableID, key), p)
}

// DeleteDataTableRow deletes one row.
func (c *Client) DeleteDataTableRow(tableID, key string) error {
	return c.deleteDataTableRow(tableID, key)
}

// DeleteDataTableRowVersion deletes one row, but only if its current Version
// equals expectedVersion.
func (c *Client) DeleteDataTableRowVersion(tableID, key string, expectedVersion int64) error {
	return c.deleteDataTableRow(tableID, key, urlParam{key: queryparams.ExpectedVersion, value: strconv.FormatInt(expectedVersion, 10)})
}

func (c *Client) deleteDataTableRow(tableID, key string, params ...urlParam) error {
	if err := checkDataTableRowArgs(tableID, key); err != nil {
		return err
	}
	return c.delete(tablesIdRowsKeyUrl(tableID, key), params...)
}

// ReplaceDataTableRows atomically replaces every row in a table.
func (c *Client) ReplaceDataTableRows(tableID string, r types.DataTableRowsReplace) (types.DataTableRowsResult, error) {
	if tableID == "" {
		return types.DataTableRowsResult{}, ErrEmptyID
	}
	return c.put[types.DataTableRowsReplace, types.DataTableRowsResult](tablesIdRowsUrl(tableID), &r)
}

// MergeDataTableRows atomically upserts and deletes many rows of a table.
func (c *Client) MergeDataTableRows(tableID string, m types.DataTableRowsMerge) (types.DataTableRowsResult, error) {
	if tableID == "" {
		return types.DataTableRowsResult{}, ErrEmptyID
	}
	return c.patch[types.DataTableRowsMerge, types.DataTableRowsResult](tablesIdRowsUrl(tableID), m)
}

// ListDataTableRows returns a page of a table's rows, ordered by the key
// column unless opts says otherwise. Filters and OrderBy in opts take column
// names.
func (c *Client) ListDataTableRows(tableID string, opts types.QueryOptions) (types.DataTableRowListResponse, error) {
	if tableID == "" {
		return types.DataTableRowListResponse{}, ErrEmptyID
	}
	opts.Filters = append(slices.Clone(opts.Filters), types.Filter{Key: "TableID", Operation: "=", Values: []any{tableID}})
	return c.post[types.QueryOptions, types.DataTableRowListResponse](LIST_TABLE_ROWS_URL, &opts)
}

func checkDataTableRowArgs(tableID, key string) error {
	if tableID == "" {
		return ErrEmptyID
	} else if key == "" {
		return ErrEmptyKey
	}
	return nil
}
