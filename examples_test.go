package eet_test

import (
	"testing"

	_ "github.com/invopop/gobl"
	eet "github.com/invopop/gobl.cz.eet"
	_ "github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/pkg/examples"
)

func TestExamples(t *testing.T) {
	examples.Run(t, "examples", *eet.Update)
}
