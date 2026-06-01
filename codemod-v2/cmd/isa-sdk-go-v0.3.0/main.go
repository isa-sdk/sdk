// Command isa-sdk-go-v0.3.0 documents the migration path from the
// per-product SDK modules (sdk/core, sdk/zyins, sdk/rapidsign,
// sdk/proxy) to the unified sdk module at v0.3.0.
//
// Import statements do NOT change. Only the consumer's go.mod
// requirement entry needs rewriting. See MIGRATION.md for the exact
// steps.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "isa-sdk-go-v0.3.0 codemod: import paths are unchanged; run the go.mod migration commands in packages/go/MIGRATION.md")
}
