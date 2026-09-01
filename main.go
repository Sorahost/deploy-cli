// Command sorahost builds a project on this machine and deploys the result to a
// SORAHOST server.
//
// See README.md, or run `sorahost --help`.
package main

import (
	"os"

	"github.com/Sorahost/deploy-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
