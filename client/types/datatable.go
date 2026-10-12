/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DataTableColumnType is the type of a column in a table.
type DataTableColumnType string

const (
	DataTableColumnString    DataTableColumnType = "string"
	DataTableColumnInt       DataTableColumnType = "int"
	DataTableColumnFloat     DataTableColumnType = "float"
	DataTableColumnBool      DataTableColumnType = "bool"
	DataTableColumnTimestamp DataTableColumnType = "timestamp"
	DataTableColumnIP        DataTableColumnType = "ip"
	DataTableColumnDuration  DataTableColumnType = "duration"
	DataTableColumnMAC       DataTableColumnType = "mac"
)

// Valid reports whether t is a known column type.
func (t DataTableColumnType) Valid() bool {
	switch t {
	case DataTableColumnString, DataTableColumnInt, DataTableColumnFloat, DataTableColumnBool,
		DataTableColumnTimestamp, DataTableColumnIP, DataTableColumnDuration, DataTableColumnMAC:
		return true
	}
	return false
}

// CanBeKey reports whether a column of type t can be a table's key column.
// Floats can't, because equality on them is unreliable.
func (t DataTableColumnType) CanBeKey() bool {
	return t.Valid() && t != DataTableColumnFloat
}

// Names of the system columns that the server maintains in every table.
const (
	DataTableColumnCreated   = "_created"
	DataTableColumnCreatedBy = "_created_by"
	DataTableColumnUpdated   = "_updated"
	DataTableColumnUpdatedBy = "_updated_by"
	DataTableColumnVersion   = "_version"
	DataTableColumnSource    = "_source"
)

// DataTableSystemColumns lists the system columns in the order they're presented.
var DataTableSystemColumns = []string{
	DataTableColumnCreated, DataTableColumnCreatedBy, DataTableColumnUpdated, DataTableColumnUpdatedBy,
	DataTableColumnVersion, DataTableColumnSource,
}

const (
	// DataTableMaxNameLength is the longest permitted table name.
	DataTableMaxNameLength = 128
	// DataTableMaxColumnNameLength is the longest permitted column name. It
	// is the PostgreSQL identifier limit.
	DataTableMaxColumnNameLength = 63
)

var (
	// Queries refer to tables by name, so names look like identifiers.
	dataTableNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	// Column names can't start with an underscore, which is reserved for
	// system columns.
	dataTableColumnNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

var (
	ErrDataTableName            = errors.New("invalid table name")
	ErrDataTableColumnName      = errors.New("invalid table column name")
	ErrDataTableColumnType      = errors.New("invalid table column type")
	ErrDataTableColumnDuplicate = errors.New("duplicate table column name")
	ErrDataTableKey             = errors.New("invalid table key")
	ErrDataTableKeyChanged      = errors.New("a table's key can't be changed")
)

// ValidateDataTableName checks that name is a valid table name: it starts
// with a letter or underscore and contains only letters, digits, underscores
// and dashes.
func ValidateDataTableName(name string) error {
	if len(name) > DataTableMaxNameLength || !dataTableNameRe.MatchString(name) {
		return fmt.Errorf("%w %q", ErrDataTableName, name)
	}
	return nil
}

// DataTableColumn is a column in a table.
type DataTableColumn struct {
	Name string
	Type DataTableColumnType
	// Indexed asks the server to maintain an index on the column. The key
	// column is always indexed.
	Indexed     bool   `json:",omitempty"`
	Description string `json:",omitempty"`
}

// Validate checks c's name and type. It doesn't check for duplicates; see ValidateDataTableColumns.
func (c DataTableColumn) Validate() error {
	if len(c.Name) > DataTableMaxColumnNameLength || !dataTableColumnNameRe.MatchString(c.Name) {
		return fmt.Errorf("%w %q", ErrDataTableColumnName, c.Name)
	}
	if !c.Type.Valid() {
		return fmt.Errorf("%w %q for column %q", ErrDataTableColumnType, c.Type, c.Name)
	}
	return nil
}

// ValidateDataTableColumns validates each column, checks that no two names
// match case-insensitively, and checks that key names one of the columns and
// that its type can be a key.
func ValidateDataTableColumns(cols []DataTableColumn, key string) error {
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		if err := c.Validate(); err != nil {
			return err
		}
		lower := strings.ToLower(c.Name)
		if seen[lower] {
			return fmt.Errorf("%w %q", ErrDataTableColumnDuplicate, c.Name)
		}
		seen[lower] = true
	}
	kc, ok := findDataTableColumn(cols, key)
	if !ok {
		return fmt.Errorf("%w: %q isn't one of the table's columns", ErrDataTableKey, key)
	} else if !kc.Type.CanBeKey() {
		return fmt.Errorf("%w: key column %q can't be of type %s", ErrDataTableKey, kc.Name, kc.Type)
	}
	return nil
}

// DataTable is the definition of a table. Its rows are read and written
// separately; see DataTableRow.
type DataTable struct {
	CommonFields

	// Columns are the table's columns, in display order, including the key
	// column. The system columns in DataTableSystemColumns are not listed.
	Columns []DataTableColumn
	// Key names the column whose values identify rows. It can't be changed
	// after the table is created.
	Key string
}

// Validate checks t's name, columns and key.
func (t DataTable) Validate() error {
	if err := ValidateDataTableName(t.Name); err != nil {
		return err
	}
	return ValidateDataTableColumns(t.Columns, t.Key)
}

// ValidateChange checks that t, which must itself be valid, can replace
// prev: the key is the same, and the key column has the same type.
func (t DataTable) ValidateChange(prev DataTable) error {
	if !strings.EqualFold(t.Key, prev.Key) {
		return fmt.Errorf("%w: from %q to %q", ErrDataTableKeyChanged, prev.Key, t.Key)
	}
	pc, _ := prev.Column(prev.Key)
	if c, _ := t.Column(t.Key); c.Type != pc.Type {
		return fmt.Errorf("%w: key column %q can't change type from %s to %s", ErrDataTableKeyChanged, c.Name, pc.Type, c.Type)
	}
	return nil
}

// Column returns the column named name, ignoring case.
func (t DataTable) Column(name string) (DataTableColumn, bool) {
	return findDataTableColumn(t.Columns, name)
}

func findDataTableColumn(cols []DataTableColumn, name string) (DataTableColumn, bool) {
	for _, c := range cols {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return DataTableColumn{}, false
}

// DataTablePatch is the type used to request an update to an existing
// DataTable. Setting Columns replaces the column list, which alters the
// table's schema; the key column must remain, with the same type. The key
// itself can't be changed.
type DataTablePatch struct {
	CommonFieldsPatch
	Columns Optional[[]DataTableColumn] `json:",omitzero"`
}

// ToPatch converts t into a DataTablePatch with every field set.
func (t DataTable) ToPatch() DataTablePatch {
	return DataTablePatch{
		CommonFieldsPatch: t.CommonFields.ToPatch(),
		Columns:           NewOptional(t.Columns),
	}
}

type DataTableListResponse struct {
	BaseListResponse
	Results []DataTable
}

// DataTableRowValues holds a row's column values, keyed by column name. Each
// value is kept as raw JSON so that it can be decoded according to its
// column's DataTableColumnType; decoding an int column into an any would lose
// precision above 2^53. Requests can name columns in any case, and responses
// use the case the table defines.
type DataTableRowValues map[string]jsontext.Value

// DataTableRow is one row of a table. Every field except Values is a system
// column that the server maintains.
type DataTableRow struct {
	// Values holds the row's column values, including the key column.
	// Columns with no value are omitted.
	Values      DataTableRowValues
	Created     time.Time
	CreatedByID int32
	Updated     time.Time
	UpdatedByID int32
	Version     int64
	Source      string
}

// DataTableRowWrite is sent to create or replace one row.
type DataTableRowWrite struct {
	// Values for the row's columns. Columns left out are set to no value.
	// The key column can be left out; if present, it must match the key the
	// row is written under.
	Values DataTableRowValues
	// ExpectedVersion, if set, makes the write conditional on the row's
	// current Version. 0 means the row must not exist yet.
	ExpectedVersion Optional[int64] `json:",omitzero"`
	// Source is recorded as the row's Source. The server defaults it to "api".
	Source string `json:",omitempty"`
}

// DataTableRowPatch is sent to change some of an existing row's columns. At
// least one of Values and Clear must name a column, and a column can't be in
// both. The key column can't be changed or cleared.
type DataTableRowPatch struct {
	// Values holds the columns to set.
	Values DataTableRowValues `json:",omitempty"`
	// Clear names the columns to clear, so that they have no value.
	Clear []string `json:",omitempty"`
	// ExpectedVersion, if set, makes the update conditional on the row's
	// current Version.
	ExpectedVersion Optional[int64] `json:",omitzero"`
	Source          string          `json:",omitempty"`
}

// DataTableRowsReplace is sent to replace every row in a table atomically.
// Every row needs a value in the key column, and key values must be unique.
type DataTableRowsReplace struct {
	Rows   []DataTableRowValues
	Source string `json:",omitempty"`
}

// DataTableRowsMerge is sent to upsert and delete many rows atomically. A key
// may not appear in both Upserts and Deletes.
type DataTableRowsMerge struct {
	// Upserts are rows to create, or to replace if their key exists. Each
	// needs a value in the key column.
	Upserts []DataTableRowValues `json:",omitempty"`
	// Deletes are the key values of rows to delete, in the key column's JSON
	// form. Keys that don't exist are ignored.
	Deletes []jsontext.Value `json:",omitempty"`
	Source  string           `json:",omitempty"`
}

// DataTableRowsResult is the outcome of a bulk row operation.
type DataTableRowsResult struct {
	Written int64 // rows created or replaced
	Deleted int64 // existing rows removed
}

type DataTableRowListResponse struct {
	BaseListResponse
	Results []DataTableRow
}
