# gwcli

A redesigned Gravwell client for the terminal, supporting both TUI-served interactivity and non-interactive script calls. 

![demo](demo.gif)

# Features

- interactive *and* scriptable

- query editor

- dynamic (paged and scrollable) viewport for interacting with query results

- multiple query result download formats (CSV, JSON, raw)

- shell-style navigation

- `tree` command to view entire structure

- command history

- context-aware help for every command

- automatic login via token (for subsequent logins)

- completions for zsh, fish, bash, and powershell

- limited tab completion in interactive mode

- pluggable framework for easily adding new capabilities (complete with genericized boilerplate and generator functions)

- `--script` flag to indicate that the program should never wait for user input or confirmation

# Usage

`./gwcli` to jump right into interactive mode or `./gwcli -h` to learn more about flags and how to use gwcli in a script.

---

The CLI can be used interactively or as a script tool.

Calling an action directly (ex: `./gwcli query tag=gravwell`) will invoke the action and return the results.

Calling gwcli bare or from a menu (ex: `./gwcli macros`) will start an interactive prompt at that directory (unless `--script` is given, in which case it will display help).

Attach `-h` to any command for full details on flags and commands.

## Login

In interactive mode (when `--script` is not supplied), gwcli will attempt to use an existing token and will prompt you for any missing credentials (username, password, TOTP).

If you are in script mode, you should use an API Token (see [Kris' blog post](https://www.gravwell.io/blog/the-basics-of-gravwell-api-access-tokens)). If your account is *not* MFA-enabled, you can also log on with `-u`/`-p` or `-u`/`--password`.

# Building

In the `./gwcli` directory, call `mage build`. If you do not have [mage](magefile.org) installed, call `go build -o gwcli .`

# Troubleshooting

## Client Not Ready For Login

Does your gravwell instance have a valid cert? If not, make sure you are using `--insecure`.

## Terminal Scrolls Back Down Every Blink/Output

This is likely a setting in your terminal.

`xterm` (and some xterm-based terminals) have it set by default; it can be disabled (at least in xterm) by unchecking the "Scroll to Bottom On Tty Output" option in your ctrl+MMB modal.

# Design

gwcli is built on the fabulous BubbleTea and Cobra libraries. In the simplest of terms, gwcli is a cobra.Command tree with a bubbletea.Model crawling around it, interacting with Gravwell via their batteries-included client library.

## See [Contributing](CONTRIBUTING.md) for a deep dive on the design philosophy and practical implementation.

# Known Issues

- Suggestions do not populate when navigation includes `..` (upward) or `~`/`/` (from home/root)

    - This is just based on how bubbletea manages its suggestions. It operates off a list of strings, where each string is a *complete command*, returning closest-match. The suggestion engine is not intelligent enough to "walk" the current prompt (like the walk() subroutine is). We simple recur downward and supply these strings on every move. We do not supply suggestions with upward or rooted navigation because it would add a boatload more iterations and recusive logic when building the suggestions each hop. Relately, Bubble Tea does not handle .SetSuggestions coming in asyncronously, meaning each hop has to determine its suggestions immediately so keeping the lists down improves responsiveness.