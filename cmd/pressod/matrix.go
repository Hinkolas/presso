package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Hinkolas/presso/internal/compat"
)

// runRuntimeMatrix prints the runtime images CI has to build, derived from
// the embedded compatibility matrix so the registry holds exactly the tags
// the engine can request. Output is a GitHub Actions strategy matrix
// ({"include":[...]}); --bases lists the per-PHP base images instead. No
// configuration is read: the matrix is a property of the binary.
func runRuntimeMatrix(args []string) error {
	fs := flag.NewFlagSet("runtime-matrix", flag.ContinueOnError)
	legacy := fs.Bool("legacy", true, "include the nginx + PHP-FPM images (PHP 7.4-8.1)")
	bases := fs.Bool("bases", false, "print the base images (one per PHP version) instead of the full matrix")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cat, err := compat.New(compat.Default, compat.Options{ImageTemplate: "presso-runtime:php{php}-wp{wp}", LegacyEnabled: *legacy})
	if err != nil {
		return err
	}
	var include any = cat.Pairs()
	if *bases {
		include = cat.Bases()
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(map[string]any{"include": include}); err != nil {
		return fmt.Errorf("runtime-matrix: %w", err)
	}
	return nil
}
