// release-version validates a publication tag using the engine's version policy
// and prints its prerelease flag. It performs no Git or publication operations.
package main

import (
	"fmt"
	"os"

	"github.com/Hinkolas/presso/internal/version"
)

func main() {
	if len(os.Args) != 2 || version.ReleaseOf(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "version must be vX.Y.Z or vX.Y.Z-{alpha,beta,rc}.N; no leading zeros, build metadata, or undotted suffixes such as -rc1")
		os.Exit(1)
	}
	fmt.Println(version.IsPrerelease(os.Args[1]))
}
