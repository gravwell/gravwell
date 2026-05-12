/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package traverse manages subroutines that traverse a command tree (or assist in doing so).
// Ensures consistency should magic symbols (like "~") change.
package traverse

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/shlex"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/cobra"
)

const (
	// RootToken navigates to the root of a command tree
	RootToken string = "~"
	// RootTokenSecondary navigates to the root of a command tree.
	RootTokenSecondary string = "/"
	// UpToken navigate up one directory from pwd.
	// Use Up() for traversal.
	UpToken string = ".."
)

// Up return the parent directory to the given command.
// Returns itself if it has no parent.
func Up(dir *cobra.Command) *cobra.Command {
	if dir.Parent() == nil { // if we are at root, do nothing
		return dir
	}
	// otherwise, step upward
	return dir.Parent()
}

// IsRootTraversalToken returns true iff the given string is exactly "~" or "/".
func IsRootTraversalToken(tkn string) bool {
	return tkn == RootToken || tkn == RootTokenSecondary
}

// IsUpTraversalToken returns true iff the given string is exactly "..".
func IsUpTraversalToken(tkn string) bool {
	return tkn == ".."
}

//#region suggestion engine

// A Suggestion is a possible completion for the given input.
type Suggestion struct {
	FullName          string
	MatchedCharacters string // characters in CmdName that the input's suggestion chunk matched
}

// Equals compares against a given CmdSuggestion, checking that the name and matching characters are equal.
func (cs Suggestion) Equals(b Suggestion) bool {
	return cs.FullName == b.FullName && cs.MatchedCharacters == b.MatchedCharacters
}

// SuggestionsCompare serves as a SortFunc provider for Suggestion{}.
// String-sorts by each element's FullName.
func SuggestionsCompare(i, j Suggestion) int {
	return strings.Compare(i.FullName, j.FullName)
}

// DeriveSuggestions walks a command tree, starting at the given WD, to identify possible completions/suggestions based on the input fragment.
// Aliases are not suggested, but can be used to traverse the tree to find suggestions for subcommands.
// The special traversal characters are returned as matching BIs.
//
// DeriveSuggestions serves as a data layer and expects the caller to enact their desired formatting/visualization.
//
// Returns suggestions based on navs, actions, and bis. Each slice is sorted via strings.Compare() on FullName.
// Returns all local suggestions if the suggest token is empty.
// Returns nothing if startingWD is nil or an action or builtin are found during traversal.
//
// ! Comparisons are case-sensitive.
func DeriveSuggestions(curInput string, startingWD *cobra.Command, builtins []string) (navs, actions, bis []Suggestion) {
	if startingWD == nil {
		return
	}
	// shift the last token to split traversal and suggestion segments
	//
	// The first chunk is the traversal chunk containing all but the last element.
	// It is used to navigate the tree.
	//
	// The second chunk is the final token and serves as basis for suggestions.
	// If it is empty, all current options will be shown.
	var (
		traversal []string
		suggest   string
	)
	if strings.TrimSpace(curInput) != "" { // break into segments only if visible characters were given
		exploded := strings.Split(curInput, " ")
		switch {
		case len(exploded) > 0:
			suggest = exploded[len(exploded)-1] // only the final token matters for suggestions
			fallthrough
		case len(exploded) > 1:
			// everything else is traversal
			traversal = exploded[0 : len(exploded)-1]
		}
	}

	// --- begin traversal stage ---
	pwd := startingWD
	// loop attempts to walk navs and special traversal tokens.
	// if at any point it fails to match a word/token, the whole suggestion engine gives up
word:
	for _, word := range traversal {
		// check for special traversal token match
		if IsRootTraversalToken(word) {
			pwd = pwd.Root()
			continue word
		} else if IsUpTraversalToken(word) {
			pwd = Up(pwd)
			continue word
		}
		// treat the help action as a no-op
		if word == "help" {
			continue
		}
		// check for command match
		for _, cmd := range pwd.Commands() {
			if cmd.Name() == word || slices.ContainsFunc(cmd.Aliases, func(alias string) bool { return alias == word }) {
				if cmd.GroupID == group.NavID { // matching nav, updated wd
					pwd = cmd
					continue word
				}
				// matching action, die
				return nil, nil, nil
			}
		}
		// if we made it this far, we matched nothing, an action, or a bi.
		// In any case, we have no work left to do.
		return nil, nil, nil
	}

	// --- begin suggestion stage ---
	var all = strings.TrimSpace(suggest) == ""
	// if suggestion is empty, suggest all items
	// collect suggestions using the context uncovered by the traversal stage
	// can be marginally parallelized
	var wg sync.WaitGroup
	wg.Go(func() { // check against builtins
		// treat the special traversal tokens as builtins
		builtins = append(builtins, RootToken, RootTokenSecondary, UpToken)
		for _, bi := range builtins {
			if sgt, match := prefixMatch(all, bi, suggest); match {
				bis = append(bis, sgt)
			}
		}
		slices.SortStableFunc(bis, SuggestionsCompare)
	})
	wg.Go(func() {
		children := pwd.Commands()
		// check against each command's name
		for _, cmd := range children {
			if sgt, match := prefixMatch(all, cmd.Name(), suggest); match {
				if cmd.GroupID == group.NavID {
					navs = append(navs, sgt)
				} else { // default to treating unknowns as actions
					actions = append(actions, sgt)
				}
			}
		}
		slices.SortStableFunc(navs, SuggestionsCompare)
		slices.SortStableFunc(actions, SuggestionsCompare)
	})

	wg.Wait()

	return
}

// helper/clarity function for DeriveSuggestions.
// prefixMatch returns the suggestion if we are in all mode or word prefix-matched frag.
func prefixMatch(all bool, word, frag string) (_ Suggestion, match bool) {
	s := Suggestion{FullName: word}
	if !all {
		// check for matching characters
		if _, found := strings.CutPrefix(word, frag); !found {
			return Suggestion{}, false
		}
		s.MatchedCharacters = frag
	}
	// if we made it this far, then it is a valid suggestion
	return s, true
}

//#endregion suggestion engine

//#region walk

// WalkResult is the outcome of a Walk() call.
// It represents the properties found from parsing a user input string.
// Builtin should not contain "Help" unless HelpMode is also set.
// This is because HelpMode represents that the caller should invoke help;
// if Builtin contains Help, then it is because the user activated HelpMode on the "help" builtin.
type WalkResult struct {
	EndCmd          *cobra.Command // the last nav or action seen.
	RemainingTokens []string       // all tokens remaining after endCmd
	Builtin         string         // the builtin to trigger; it will only contain "help" if HelpMode is also set (requesting help about help).
	HelpMode        bool           // display help for the endCmd or builtin, rather than invoking it
}

// Walk traverses the given user input and returns how to handle it (and whether or not it is erroneous).
// It assumes input has the form ["help"] <command path> [flags] and will error if this form is not met.
// Parsing stops when a flag is found, an action is found, no tokens remain, or an error occurred.
// If an error is returned, WalkResult will contain the state of Walk when the error was encountered.
func Walk(pwd *cobra.Command, input string, builtinActions []string) (WalkResult, error) {
	if pwd == nil {
		return WalkResult{}, errors.New("pwd cannot be nil")
	} else if input == "" {
		return WalkResult{}, nil
	}

	// setup
	var wg sync.WaitGroup

	// transmute builtins to a hashset
	wg.Add(1)
	var biSet map[string]bool
	go func() {
		defer wg.Done()
		biSet = make(map[string]bool, len(builtinActions))
		for _, biAct := range builtinActions {
			biSet[biAct] = true
		}
	}()

	// split input
	wg.Add(1)
	var (
		tokens []string
		err    error
	)
	go func() {
		defer wg.Done()
		tokens, err = shlex.Split(strings.TrimSpace(input))
	}()
	wg.Wait()

	if err != nil {
		return WalkResult{}, err
	} else if len(tokens) < 1 {
		return WalkResult{
			EndCmd: pwd,
		}, nil
	}

	// check for "help" mode or help flags on pwd
	var helpMode bool
	switch tokens[0] {
	case "-h", "--help":
		return WalkResult{EndCmd: pwd, RemainingTokens: tokens[1:], HelpMode: true}, nil
	case "help":
		helpMode = true
		// check for "help help"
		if len(tokens[1:]) > 0 && tokens[1] == "help" {
			return WalkResult{
				EndCmd:          nil,
				RemainingTokens: tokens[2:],
				Builtin:         "help",
				HelpMode:        true,
			}, nil
		}
		tokens = tokens[1:]
	}

	endCmd, excessTokens, builtin, unknownToken := findEndCommand(pwd, slices.Clip(tokens), biSet)
	// transform the results into a WalkResult
	wr := WalkResult{
		EndCmd:          endCmd,
		RemainingTokens: excessTokens,
		Builtin:         builtin,
	}
	// check for errors
	if unknownToken != "" {
		return wr, errors.New(unknownToken + " is not a valid builtin or subcommand")
	} else if builtin == "help" {
		// we explicitly check for help prior to findEndCommand.
		// if it was found again, then this must have been bad input
		return wr, fmt.Errorf("help must be of the form %v. See %v for more help",
			stylesheet.Cur.ExampleText.Render("help "+ft.MutuallyExclusive([]string{"command path"})),
			stylesheet.Cur.ExampleText.Render("help help"))
	}
	// look ahead for -h/--help
	if slices.ContainsFunc(excessTokens, func(tkn string) bool { return tkn == "-h" || tkn == "--help" }) {
		helpMode = true
		// clip out the help flags so remaining tokens is consistent
		wr.RemainingTokens = slices.DeleteFunc(excessTokens, func(tkn string) bool { return tkn == "-h" || tkn == "--help" })
	}
	wr.HelpMode = helpMode
	return wr, nil
}

// findEndCommand is the underlying, recursive driver for Walk.
// It traverses tokens to identify what nav, action, or builtin the user was attempting to invoke.
// Stops on the first flag, action, or builtin it finds.
//
// pwd is our current position.
// remainingTokens is the shlex'd tokens that have not yet been processed.
// builtins is a hashset of builtin action names.
//
// end is the last valid cobra command found. It will always be populated.
// excessTokens is extra tokens remaining post-traversal.
// builtinInvoked is the name of the builtin to be invoked. Will be empty if the user did not invoke a builtin.
// unknownToken is the non-flag token that stopped processing. Flags stop processing without returning unknown token.
func findEndCommand(pwd *cobra.Command, remainingTokens []string, builtins map[string]bool) (end *cobra.Command, excessTokens []string, builtinInvoked string, unknownToken string) {
	if len(remainingTokens) == 0 { // nothing left to parse, return current state
		return pwd, nil, "", ""
	}
	// cut the first token
	curTkn, remainingTokens := strings.TrimSpace(remainingTokens[0]), remainingTokens[1:]
	if curTkn == "" { // ignore extra whitespace
		return findEndCommand(pwd, remainingTokens, builtins)
	} else if curTkn[0] == '-' { // found a flag or flag-like token
		// reattach the flag
		return pwd, append([]string{curTkn}, remainingTokens...), "", ""
	}
	// special tokens have the highest priority
	if IsUpTraversalToken(curTkn) {
		return findEndCommand(Up(pwd), remainingTokens, builtins)
	} else if IsRootTraversalToken(curTkn) {
		return findEndCommand(pwd.Root(), remainingTokens, builtins)

	}
	// child commands have next highest priority
	for _, child := range pwd.Commands() {
		if child.Name() == curTkn || child.HasAlias(curTkn) {
			if action.Is(child) {
				return child, remainingTokens, "", ""
			}
			// keep traversing navs
			return findEndCommand(child, remainingTokens, builtins)
		}
	}
	if _, found := builtins[curTkn]; found {
		return pwd, remainingTokens, curTkn, ""
	}

	return pwd, remainingTokens, "", curTkn
}

//#endregion walk
