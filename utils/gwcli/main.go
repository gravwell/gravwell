/**
 * gwcli is driven by mother/root.go's .Execute() method, which is called here
 */

package main

import (
	"gwcli/tree"
	"os"
)

func init() {
}

func main() {
	os.Exit(tree.Execute(nil))
}
