// Command otakase-core is the desktop app's helper process. It serves the
// mobile core over stdin and stdout (see package mobilecore/stdio) and exits
// when the app closes its stdin.
package main

import (
	"fmt"
	"os"

	"github.com/thexykril/otakase/mobilecore"
	"github.com/thexykril/otakase/mobilecore/stdio"
)

func main() {
	if err := stdio.Serve(mobilecore.NewCore(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "otakase-core:", err)
		os.Exit(1)
	}
}
