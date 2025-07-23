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
	"os"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

// Given a **parsed** flagset, determines and returns output format.
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
	if fm, err := fs.GetBool(ft.CSV.Name); err != nil {
		uniques.ErrGetFlag("list", err)
		// non-fatal
	} else if fm {
		return csv
	}

	// check for JSON
	if fm, err := fs.GetBool(ft.JSON.Name); err != nil {
		uniques.ErrGetFlag("list", err)
	} else if fm {
		format = json
	}

	// if we made it this far, return the default
	return format
}

// Driver function to fetch the list output.
// Determines what (pre)processing is required to retrieve output for the given format and does so, returning the formatted string.
func listOutput[retStruct any](
	fs *pflag.FlagSet,
	format outputFormat,
	columns []string,
	dataFn ListDataFunction[retStruct],
	prettyFunc PrettyPrinterFunc,
	aliases map[string]string,
) (string, error) {
	// hand off control to pretty
	if format == pretty {
		if prettyFunc == nil {
			return "", errors.New("format is pretty, but prettyFunc is nil")
		}
		return prettyFunc(fs)
	}

	// massage the data for weave
	data, err := dataFn(fs)
	if err != nil {
		return "", err
	} else if len(data) < 1 {
		return "", nil
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
		toRet = weave.ToTable(data, columns, weave.TableOptions{
			Base:    stylesheet.Table,
			Aliases: aliases,
		})
	default:
		toRet = ""
		err = fmt.Errorf("unknown output format (%d)", format)
	}
	return toRet, err
}

// buildFlagSet constructs and returns a flagset composed of the default list flags, additional flags defined for this action, and --pretty if a prettyFunc was defined.
func buildFlagSet(afs AddtlFlagFunction, prettyDefined bool) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.Bool(ft.CSV.Name, false, ft.CSV.Usage)
	fs.Bool(ft.JSON.Name, false, ft.JSON.Usage)
	fs.Bool(ft.Table.Name, true, ft.Table.Usage) // default
	fs.StringSlice(ft.Name.SelectColumns, []string{}, ft.Usage.SelectColumns)
	fs.Bool("show-columns", false, "display the list of fully qualified column names and die.")
	fs.StringP(ft.Output.Name, "o", "", ft.Output.Usage)
	fs.Bool(ft.Append.Name, false, ft.Append.Usage)
	fs.Bool(ft.Name.AllColumns, false, ft.Usage.AllColumns)
	// if prettyFunc was defined, bolt on pretty
	if prettyDefined {
		fs.Bool("pretty", false, "display results as prettified text.\n"+
			"Takes precedence over other format flags.")
	}
	// if additional flags are warranted, add them
	if afs != nil {
		a := afs()
		fs.AddFlagSet(&a)
	}

	return &fs

}

// Opens a file, per the given --output and --append flags in the flagset, and returns its handle.
// Returns nil if the flags do not call for a file.
func initOutFile(fs *pflag.FlagSet) (*os.File, error) {
	if !fs.Parsed() {
		return nil, nil
	}
	outPath, err := fs.GetString(ft.Output.Name)
	if err != nil {
		return nil, err
	} else if strings.TrimSpace(outPath) == "" {
		return nil, nil
	}
	var flags = os.O_CREATE | os.O_WRONLY
	if append, err := fs.GetBool(ft.Append.Name); err != nil {
		return nil, err
	} else if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(outPath, flags, outFilePerm)
}

// getColumns checks for --columns then validates and returns them if found and returns the default columns otherwise.
func getColumns(fs *pflag.FlagSet, defaultColumns []string, availDSColumns []string) ([]string, error) {
	if all, err := fs.GetBool(ft.Name.AllColumns); err != nil {
		return nil, uniques.ErrGetFlag("list", err) // does not return the actual 'use' of the action, but I don't want to include it as a param just for this super rare case
	} else if all {
		return availDSColumns, nil
	}
	cols, err := fs.GetStringSlice(ft.Name.SelectColumns)
	if err != nil {
		return nil, uniques.ErrGetFlag("list", err) // does not return the actual 'use' of the action, but I don't want to include it as a param just for this super rare case
	} else if len(cols) < 1 {
		return defaultColumns, nil
	}

	if err := validateColumns(cols, availDSColumns); err != nil {
		return nil, err
	}
	return cols, nil
}

// validateColumns tests that every given column exists within the given struct.
func validateColumns(cols []string, availDSColumns []string) error {
	// transform the DS columns into a map for faster access
	m := make(map[string]bool, len(availDSColumns))
	for _, col := range availDSColumns {
		m[col] = true
	}

	// confirm that each column is an existing column
	for _, col := range cols {
		if _, found := m[col]; !found {
			return fmt.Errorf("'%v' is not a known column", col)
		}
	}

	return nil
}
