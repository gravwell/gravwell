# repo tools

A Go tool for repo-wide operations. Run via:

```
go tool repo <command> [args]
```

Must be run from the repo root.

## Adding a new command

1. Add a function with the signature `func myCommand(args []string) error`.
2. Register it in the `switch` block in `main()`:
```go
   case "my-command":
       err = myCommand(os.Args[2:])
```

The `mods` slice at the top of `main.go` lists all Go module paths (relative to repo root). Add new modules there if needed.
Any new go version references should be added to `runtimes` as well to ensure they are updated with the command.
