// Command dump prints the embedded release catalog as a JSON array
// (scripts/release.sh writes it to share/twd/catalog.json).
package main

import (
	"encoding/json"
	"os"

	"github.com/rosskukulinski/twd/internal/catalog"
)

func main() {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(catalog.Embedded()); err != nil {
		os.Exit(1)
	}
}
