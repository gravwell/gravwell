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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
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
		return tbl
	}
	var format = tbl   // default to tbl
	if prettyDefined { // if defined, default to pretty and check for explicit flag
		format = pretty
		if fm, err := fs.GetBool("pretty"); err != nil {
			clilog.Writer.Criticalf("failed to fetch --pretty despite believing prettyFunc to be defined: %v", err)
		} else if fm {
			// manually declared, use it
			return pretty
		}
	}
	// check for CSV
	if fm, err := fs.GetBool(ft.CSV.Name()); err != nil {
		uniques.ErrGetFlag("list", err)
		// non-fatal
	} else if fm {
		return csv
	}

	// check for JSON
	if fm, err := fs.GetBool(ft.JSON.Name()); err != nil {
		uniques.ErrGetFlag("list", err)
	} else if fm {
		return json
	}

	// check for explicit table
	if fm, err := fs.GetBool(ft.Table.Name()); err != nil {
		uniques.ErrGetFlag("list", err)
		// non-fatal
	} else if fm {
		return tbl
	}

	// if we made it this far, return the default
	return format
}

// Driver function to fetch the list output.
// Determines what (pre)processing is required to retrieve output for the given format and does so, returning the formatted string.
func listOutput[struct_t any](
	fs *pflag.FlagSet,
	format outputFormat,
	columns []string,
	dataFunc ListDataFunc[struct_t],
	prettyFunc PrettyPrinterFunc,
	DQToAlias map[string]string,
) (string, error) {
	// hand off control to pretty
	if format == pretty {
		if prettyFunc == nil {
			return "", errors.New("format is pretty, but prettyFunc is nil")
		}
		return prettyFunc(columns, DQToAlias)
	}

	// massage the data for weave
	data, err := dataFunc(fs)
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
	case csv:
		toRet = weave.ToCSV(data, columns, weave.CSVOptions{Aliases: aliases})
	case json:
		toRet, err = weave.ToJSON(data, columns, weave.JSONOptions{Aliases: aliases})
	case tbl:
		toRet = weave.ToTable(data, columns, weave.TableOptions{Base: stylesheet.Table, Aliases: aliases})
	default:
		toRet = ""
		err = fmt.Errorf("unknown output format (%d)", format)
	}
	return toRet, err
}

// buildFlagSet constructs and returns a flagset composed of the default list flags, additional flags defined for this action, and --pretty if a prettyFunc was defined.
func buildFlagSet(prettyDefined bool) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	ft.CSV.Register(&fs)
	ft.JSON.Register(&fs)
	ft.Table.Register(&fs)
	//fs.Bool(ft.Table.Name(), true, ft.Table.Usage()) // default
	ft.SelectColumns.Register(&fs)
	ft.ShowColumns.Register(&fs)

	ft.Output.Register(&fs)
	ft.Append.Register(&fs)
	ft.AllColumns.Register(&fs)

	// if prettyFunc was defined, bolt on pretty
	if prettyDefined {
		fs.Bool("pretty", false, "display results as prettified text.\n"+
			"Takes precedence over other format flags.\n"+
			"May or may not respect columns, default or selected via --"+ft.SelectColumns.Name()+".")
	}

	return &fs

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
	if append, err := fs.GetBool(ft.Append.Name()); err != nil {
		return nil, err
	} else if append {
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

// getColumns figures out which columns should be used for this request.
// In order of priority:
//
//  1. all columns (if --all)
//
//  2. selected columns (if --columns=<>)
//
//  3. default columns
func getColumns(fs *pflag.FlagSet, DQToAlias, AliasToDQ map[string]string, defaultColumns []string) ([]string, error) {
	if all, err := fs.GetBool(ft.AllColumns.Name()); err != nil {
		return nil, uniques.ErrGetFlag("list", err) // does not return the actual 'use' of the action, but I don't want to include it as a param just for this super rare case
	} else if all {
		return slices.Collect(maps.Keys(DQToAlias)), nil
	}
	if selectedCols, err := fs.GetStringSlice(ft.SelectColumns.Name()); err != nil {
		return nil, uniques.ErrGetFlag("list", err) // does not return the actual 'use' of the action, but I don't want to include it as a param just for this super rare case
	} else if len(selectedCols) > 0 { // if columns were selected, validate the request and return the set
		normalized, unknown := normalizeColumns(selectedCols, DQToAlias, AliasToDQ)
		if len(unknown) > 0 {
			return nil, fmt.Errorf("--%s has unknown columns/aliases: %v", ft.SelectColumns.Name(), unknown)
		}
		return normalized, nil
	}

	// neither --all nor --columns=<> was not specified; return defaults
	return defaultColumns, nil
}

// validateColumns tests that every given column exists within the given struct.
// Returns the list of unknown columns.
func validateColumns(cols []string, availDSColumns []string) (unknown []string) {
	// transform the DS columns into a map for faster access
	m := make(map[string]bool, len(availDSColumns))
	for _, col := range availDSColumns {
		m[col] = true
	}

	// confirm that each column is an existing column
	for _, col := range cols {
		if _, found := m[col]; !found {
			unknown = append(unknown, col)
		}
	}

	return unknown
}
