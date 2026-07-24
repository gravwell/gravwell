package weave

import "github.com/charmbracelet/lipgloss/table"

// TableOptions defines a set of modifiers that ToTable can take into account at Render time.
// It is safe to pass an empty TableOptions struct.
type TableOptions struct {
	// Base returns a stylized table to use as the base so you can apply styles and wraps.
	// Uses the table.New() defaults if nil.
	Base func() *table.Table
	// Aliases maps fully-dot-qualified field names -> display names.
	// Keys must exactly match native column names (from StructFields());
	// unmatched aliases will be unused and native column names are case-sensitive.
	// When writing headers, ToTable will prefer an Alias, if found.
	// Operates in O(len(columns)) time, if not nil.
	Aliases map[string]string
}

// CSVOptions defines a set of modifiers that ToCSV can take into account at Render time.
// It is safe to pass an empty CSVOptions struct.
type CSVOptions struct {
	// Aliases maps fully-dot-qualified field names -> display names.
	// Keys must exactly match native column names (from StructFields());
	// unmatched aliases will be unused and native column names are case-sensitive.
	// When writing headers, ToCSV will prefer an alias, if found.
	Aliases map[string]string
}

// JSONOptions defines a set of modifiers that ToCSV can take into account at Render time.
// It is safe to pass an empty CSVOptions struct.
type JSONOptions struct {
	// Aliases maps fully-dot-qualified field names -> display names.
	// Keys must exactly match native column names (from StructFields());
	// unmatched aliases will be unused and native column names are case-sensitive.
	// When writing headers, ToJSON will prefer an alias, if found.
	//
	// As "." is illegal in JSON keys, including a "." in the alias implies a nesting.
	// Ex: `A.B` will result in `A:{"B":<value>}`
	Aliases map[string]string
}
