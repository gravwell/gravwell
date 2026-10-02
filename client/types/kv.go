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

// KVColumnType is the type of a user-defined column in a kv table.
type KVColumnType string

const (
	KVColumnString    KVColumnType = "string"
	KVColumnInt       KVColumnType = "int"
	KVColumnFloat     KVColumnType = "float"
	KVColumnBool      KVColumnType = "bool"
	KVColumnTimestamp KVColumnType = "timestamp"
	KVColumnIP        KVColumnType = "ip"
	KVColumnBytes     KVColumnType = "bytes"
)

// Valid reports whether t is a known column type.
func (t KVColumnType) Valid() bool {
	switch t {
	case KVColumnString, KVColumnInt, KVColumnFloat, KVColumnBool, KVColumnTimestamp, KVColumnIP, KVColumnBytes:
		return true
	}
	return false
}

// Names of the system columns that kv maintains in every table.
const (
	KVColumnKey       = "key"
	KVColumnCreated   = "_created"
	KVColumnCreatedBy = "_created_by"
	KVColumnUpdated   = "_updated"
	KVColumnUpdatedBy = "_updated_by"
	KVColumnVersion   = "_version"
	KVColumnSource    = "_source"
)

// KVSystemColumns lists the system columns in the order kv presents them.
var KVSystemColumns = []string{
	KVColumnKey, KVColumnCreated, KVColumnCreatedBy, KVColumnUpdated, KVColumnUpdatedBy, KVColumnVersion, KVColumnSource,
}

// KVMaxColumnNameLength is the longest permitted column name. It is the
// PostgreSQL identifier limit.
const KVMaxColumnNameLength = 63

var kvColumnNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

var (
	ErrKVColumnName      = errors.New("invalid kv column name")
	ErrKVColumnReserved  = errors.New("kv column name is reserved")
	ErrKVColumnType      = errors.New("invalid kv column type")
	ErrKVColumnDuplicate = errors.New("duplicate kv column name")
)

// KVColumn is a user-defined column in a kv table.
type KVColumn struct {
	Name        string
	Type        KVColumnType
	Indexed     bool   `json:",omitempty"`
	Description string `json:",omitempty"`
}

// Validate checks c's name and type. It doesn't check for duplicates; see ValidateKVColumns.
func (c KVColumn) Validate() error {
	if len(c.Name) > KVMaxColumnNameLength || !kvColumnNameRe.MatchString(c.Name) {
		return fmt.Errorf("%w %q", ErrKVColumnName, c.Name)
	}
	if strings.EqualFold(c.Name, KVColumnKey) {
		return fmt.Errorf("%w: %q", ErrKVColumnReserved, c.Name)
	}
	if !c.Type.Valid() {
		return fmt.Errorf("%w %q for column %q", ErrKVColumnType, c.Type, c.Name)
	}
	return nil
}

// ValidateKVColumns validates each column and checks that no two names match
// case-insensitively.
func ValidateKVColumns(cols []KVColumn) error {
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		if err := c.Validate(); err != nil {
			return err
		}
		lower := strings.ToLower(c.Name)
		if seen[lower] {
			return fmt.Errorf("%w %q", ErrKVColumnDuplicate, c.Name)
		}
		seen[lower] = true
	}
	return nil
}

// KVTable is the definition of a kv table. Its rows are read and written
// separately; see KVTableRow.
type KVTable struct {
	CommonFields

	// Columns are the user-defined columns, in display order. The system
	// columns in KVSystemColumns are not listed.
	Columns []KVColumn
}

// KVTablePatch is the type used to request an update to an existing KVTable.
// Setting Columns replaces the column list, which alters the table's schema.
type KVTablePatch struct {
	CommonFieldsPatch
	Columns Optional[[]KVColumn] `json:",omitzero"`
}

// ToPatch converts t into a KVTablePatch with every field set.
func (t KVTable) ToPatch() KVTablePatch {
	return KVTablePatch{
		CommonFieldsPatch: t.CommonFields.ToPatch(),
		Columns:           NewOptional(t.Columns),
	}
}

type KVTableListResponse struct {
	BaseListResponse
	Results []KVTable
}

// KVValues holds a row's user column values, keyed by column name. Each value
// is kept as raw JSON so that it can be decoded according to its column's
// KVColumnType; decoding an int column into an any would lose precision above
// 2^53. In a KVTableRowPatch, a JSON null clears the column.
type KVValues map[string]jsontext.Value

// KVTableRow is one row of a kv table. Every field except Values is a system
// column that kv maintains.
type KVTableRow struct {
	Key         string
	Values      KVValues
	Created     time.Time
	CreatedByID int32
	Updated     time.Time
	UpdatedByID int32
	Version     int64
	Source      string
}

// KVTableRowWrite is sent to create or replace one row.
type KVTableRowWrite struct {
	// Values for the row's user columns. Columns left out are set to no value.
	Values KVValues
	// ExpectedVersion, if set, makes the write conditional on the row's
	// current Version. 0 means the row must not exist yet.
	ExpectedVersion Optional[int64] `json:",omitzero"`
	// Source is recorded as the row's Source. The server defaults it to "api".
	Source string `json:",omitempty"`
}

// KVTableRowPatch is sent to change some of an existing row's columns.
type KVTableRowPatch struct {
	// Values holds the columns to change. A JSON null clears the column.
	Values KVValues
	// ExpectedVersion, if set, makes the update conditional on the row's
	// current Version.
	ExpectedVersion Optional[int64] `json:",omitzero"`
	Source          string          `json:",omitempty"`
}

// KVTableRowInput is a row to write as part of a bulk operation.
type KVTableRowInput struct {
	Key    string
	Values KVValues
}

// KVTableRowsReplace is sent to replace every row in a table atomically.
type KVTableRowsReplace struct {
	Rows   []KVTableRowInput
	Source string `json:",omitempty"`
}

// KVTableRowsMerge is sent to upsert and delete many rows atomically. A key
// may not appear in both Upserts and Deletes.
type KVTableRowsMerge struct {
	Upserts []KVTableRowInput `json:",omitempty"`
	Deletes []string          `json:",omitempty"`
	Source  string            `json:",omitempty"`
}

// KVTableRowsResult is the outcome of a bulk row operation.
type KVTableRowsResult struct {
	Written int64 // rows created or replaced
	Deleted int64 // existing rows removed
}

type KVTableRowListResponse struct {
	BaseListResponse
	Results []KVTableRow
}
