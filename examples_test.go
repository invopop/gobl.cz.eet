package eet

import (
	"flag"
	"testing"

	_ "github.com/invopop/gobl"
	_ "github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/pkg/examples"
)

// update rewrites the golden output of the tests that have one.
var update = flag.Bool("update", false, "update golden test output")

func TestExamples(t *testing.T) {
	examples.Run(t, "examples", *update)
}
