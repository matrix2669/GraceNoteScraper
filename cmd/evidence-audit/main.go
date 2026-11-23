// evidence-audit reports the non-destructive provider evidence policy against a
// caller-selected index copy. It has no apply mode and never opens network clients.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: evidence-audit <index-copy.json>")
		os.Exit(2)
	}
	r, e := lineupindex.AuditIndexFile(os.Args[1])
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if e = json.NewEncoder(os.Stdout).Encode(r); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
