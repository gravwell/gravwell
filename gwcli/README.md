# gwcli

A redesigned Gravwell client. Powerful enough to include in scripts, friendly enough to use interactively.

![demo](demo.gif)

# Features

- Call it bare to get a fancy TUI or pass the `--no-interactive` flag to indicate that the program should never wait for user input or confirmation.

- interactive query editor with dynamic (paged and scrollable) viewport for interacting with results

- download datasets and query results in a variety of formats (JSON, CSV, raw, or fancy table)

- shell-style navigation with a custom suggestion/tab completion engine

- context-aware help for every command and builtin action

- per-session command history

- variety of login options: username/password (with optional MFA), auto-login via token, script-friendly authentication via API key, and even sessions

- completions for zsh, fish, bash, and powershell

- pluggable framework for easily adding new capabilities (complete with genericized boilerplate and generator functions)

# Usage

`./gwcli` to jump right into interactive mode or `./gwcli -h` to learn more about flags and how to use gwcli in a script.

---

The CLI can be used interactively or as a script tool.

Calling an action directly (ex: `./gwcli query tag=gravwell`) will invoke the action and return the results.

Calling gwcli bare or from a menu (ex: `./gwcli macros`) will start an interactive prompt at that directory (unless `--no-interactive` is given, in which case it will display help).

Attach `-h` to any command for full details on flags and subcommands.

## Login

In interactive mode, gwcli will attempt to use an existing session token and will prompt you for any missing credentials (username, password, TOTP).

If you are in no-interactive mode, you should use an API Token (see [Kris' blog post](https://www.gravwell.io/blog/the-basics-of-gravwell-api-access-tokens)). If your account is *not* MFA-enabled, you can also log on with username and password (see `-u`).

# Building

In the `./gwcli` directory, call `mage build`. If you do not have [mage](magefile.org) installed, call `go build -o gwcli .`

# Troubleshooting

## Client Not Ready For Login

Does your gravwell instance have a valid cert? If not, make sure you are using `--insecure`.

## Terminal Scrolls Back Down Every Blink/Output

This is likely a setting in your terminal.

`xterm` (and some xterm-based terminals) have it set by default; it can be disabled (at least in xterm) by unchecking the "Scroll to Bottom On Tty Output" option in your ctrl+MMB modal.

## See [Contributing](CONTRIBUTING.md) for a deep dive on the design philosophy and implementation.