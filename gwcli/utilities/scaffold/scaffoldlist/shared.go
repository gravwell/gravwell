/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

// Given a **parsed** flagset, determines and returns output format.
// If no format flags are found, pretty is selected if it is defined. Otherwise, table is selected.
// If multiple format flags are found, they are selected with the following precedence:
//
// pretty -> csv -> json -> tbl
//
// Logs errors, allowing execution to continue towards default.
// If an error was returned, the outputFormat is undefined.
func determineFormat(fs *pflag.FlagSet, prettyDefined bool) outputFormat {
	if !fs.Parsed() {
		clilog.Writer.Warnf("flags must be parsed prior to determining format")
		return formatTable
	}
	var format = formatTable // default to tbl
	if prettyDefined {       // if defined, default to pretty and check for explicit flag
		format = formatPretty
		if fm, err := fs.GetBool("pretty"); err != nil {
			clilog.Writer.Criticalf("failed to fetch --pretty despite believing prettyFunc to be defined: %v", err)
		} else if fm {
			// manually declared, use it
			return formatPretty
		}
	}
	// check for CSV
	if fm, err := fs.GetBool(ft.CSV.Name()); err != nil {
		clilog.GetFlag(err)
		// non-fatal
	} else if fm {
		return formatCSV
	}

	// check for JSON
	if fm, err := fs.GetBool(ft.JSON.Name()); err != nil {
		clilog.GetFlag(err)
	} else if fm {
		return formatJSON
	}

	// check for explicit table
	if fm, err := fs.GetBool(ft.Table.Name()); err != nil {
		clilog.GetFlag(err)
		// non-fatal
	} else if fm {
		return formatTable
	}

	// if we made it this far, return the default
	return format
}

// Driver function to fetch the list output.
// Determines what (pre)processing is required to retrieve output for the given format and does so, returning the formatted string.
func listOutput[struct_t any](
	fs *pflag.FlagSet,
	format outputFormat,
	dqColumns []string,
	dataFunc ListDataFunc[struct_t],
	prettyFunc PrettyPrinterFunc,
	DQToAlias map[string]string,
	omit OmitFlags,
) (string, error) {
	params := DataParameters{QueryOpts: getQueryOptions(fs, omit)}

	// hand off control to pretty
	if format == formatPretty {
		if prettyFunc == nil {
			return "", errors.New("format is pretty, but prettyFunc is nil")
		}
		return prettyFunc(fs, dqColumns, DQToAlias, params)
	}

	// massage the data for weave
	data, err := dataFunc(fs, params)
	if err != nil {
		return "", err
	}

	// weave takes aliases verbatim, so we need to dump out empty aliases from the column list
	aliases := maps.Clone(DQToAlias)
	for dq, alias := range aliases {
		if alias == "" {
			delete(aliases, dq)
		}
	}

	// hand off control
	clilog.Writer.Debugf("List: format %s | row count: %d", format, len(data))
	toRet, err := "", nil
	switch format {
	case formatCSV:
		toRet = weave.ToCSV(data, dqColumns, weave.CSVOptions{Aliases: aliases})
	case formatJSON:
		toRet, err = weave.ToJSON(data, dqColumns, weave.JSONOptions{Aliases: aliases})
	case formatTable:
		toRet = weave.ToTable(data, dqColumns, weave.TableOptions{Base: stylesheet.Table, Aliases: aliases})
	default:
		toRet = ""
		err = fmt.Errorf("unknown output format (%d)", format)
	}
	return toRet, err
}

// Opens a file, per the given --output and --append flags in the flagset, and returns its handle.
// Returns nil if the flags do not call for a file.
func initOutFile(fs *pflag.FlagSet) (*os.File, error) {
	if !fs.Parsed() {
		return nil, nil
	}
	outPath, err := fs.GetString(ft.Output.Name())
	if err != nil {
		return nil, err
	} else if strings.TrimSpace(outPath) == "" {
		return nil, nil
	}
	var flags = os.O_CREATE | os.O_WRONLY
	append, err := fs.GetBool(ft.Append.Name())
	clilog.GetFlag(err)
	if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(outPath, flags, outFilePerm)
}

// normalize columns takes a list of columns (which may be a mixture of DQ and alias) and returns the dot-qualified version of each column.
//
// DQToAlias is a map of all DQ fields mapped to their alias; DQs without an alias map to "".
//
// AliasToDQ is a map of all aliases mapped to their underlying DQ version.
//
// Returns the set of normalized (as DQ) columns. If a column is not found in either map, it is returned as unknown.
func normalizeToDQ(columns []string, DQToAlias map[string]string, AliasToDQ map[string]string) (normalized, unknown []string) {
	normalized = make([]string, 0, len(columns))
	for _, col := range columns {
		if _, found := DQToAlias[col]; found {
			normalized = append(normalized, col)
			continue
		}
		if dq, found := AliasToDQ[col]; found {
			normalized = append(normalized, dq)
			continue
		}
		// not found in either map
		unknown = append(unknown, col)
	}
	return normalized, unknown
}

// The sorting mechanism list uses when an order is not specified (ex: --columns is not given).
//
// Sorts in-place, but returns the given columns so it can be inlined.
func sortColumns(columns []string) (sorted []string) {
	slices.SortStableFunc(columns, func(a, b string) int {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
		return strings.Compare(a, b)
	})

	return columns
}

// ShowColumns lists available columns, preferring column aliases if they exist.
// Columns are sorted alphabetically.
func ShowColumns(DQToAlias map[string]string) string {
	aliased := aliasColumns(slices.Collect(maps.Keys(DQToAlias)), DQToAlias)
	sortColumns(aliased)
	return strings.Join(aliased, ShowColumnSep)
}

// aliasColumns returns columnsDQ with aliases applied when they exist.
// Columns order is maintained.
func aliasColumns(columnsDQ []string, DQToAlias map[string]string) []string {
	aliased := make([]string, len(columnsDQ))
	for i, colDQ := range columnsDQ {
		if alias, found := DQToAlias[colDQ]; found && alias != "" {
			aliased[i] = alias
		} else {
			aliased[i] = colDQ
		}
	}
	return aliased
}
