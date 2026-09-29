// Command kaynakkestirir estimates the next code measurement for every scope
// from a trailing window of timestamped measurements, with a robust
// uncertainty band and anomaly alerts.
//
// All the real work lives in the stats and model packages; this file only
// chooses the exit code.
package main

import (
	"os"

	"kaynakkestirir/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
