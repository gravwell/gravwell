# DISCLAIMER

> The code in `packages.go` and `packages_windows.go` used to be auto-generated. However, incompatibilities of `scriggo` with recent golang runtimes have made the generation of these files impossible. The current state is that we edit these files manually when doing breaking API changes.

# Build instructions (Deprecated)

```sh
rm -f packages.go
scriggo import -f packages.Scriggofile -o packages.go
```
