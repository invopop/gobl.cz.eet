package eet_test

import (
	"fmt"
	"testing"

	eet "github.com/invopop/gobl.cz.eet"
	"github.com/stretchr/testify/assert"
)

func TestErrorIsByKey(t *testing.T) {
	err := eet.ErrServer.WithCode("503").WithMessage("Service Unavailable")
	assert.EqualError(t, err, "server: 503: Service Unavailable")
	assert.ErrorIs(t, err, eet.ErrServer)
	assert.NotErrorIs(t, err, eet.ErrValidation)
	assert.ErrorIs(t, fmt.Errorf("sending: %w", err), eet.ErrServer)
	assert.Equal(t, "server", err.Key())
	assert.Empty(t, eet.ErrServer.Code(), "WithCode must not change the base error")
	assert.EqualError(t, eet.ErrServer, "server")
}

// A response error is ErrServer when its code says to send again later, and
// ErrValidation otherwise.
func TestResponseErrorErr(t *testing.T) {
	err := (&eet.ResponseError{Code: eet.ErrorCodeSignature, Message: "Neplatny podpis SOAP zpravy"}).Err()
	assert.ErrorIs(t, err, eet.ErrValidation)
	assert.EqualError(t, err, "validation: 4: Neplatny podpis SOAP zpravy")

	assert.NoError(t, (&eet.ResponseError{Code: eet.ErrorCodeVerified}).Err(), "code 0 is a verification that passed")

	err = (&eet.ResponseError{Code: eet.ErrorCodeTemporary, Message: "Docasna technicka chyba"}).Err()
	assert.ErrorIs(t, err, eet.ErrServer)
	assert.ErrorIs(t, err, eet.ErrServer.WithCode("-1"))
}

// A target with a code or a message must match those too, so a sentinel
// such as ErrOnlyPayments only matches itself, while ErrValidation
// matches every validation error.
func TestErrorIsWithCodeOrMessage(t *testing.T) {
	schema := (&eet.ResponseError{Code: eet.ErrorCodeSchema, Message: "XML zprava nevyhovela kontrole XML schematu"}).Err()
	assert.ErrorIs(t, schema, eet.ErrValidation)
	assert.NotErrorIs(t, schema, eet.ErrOnlyPayments)
	assert.NotErrorIs(t, eet.ErrValidation.WithMessage("no envelope"), eet.ErrOnlyPayments)

	assert.ErrorIs(t, eet.ErrOnlyPayments, eet.ErrOnlyPayments)
	assert.ErrorIs(t, eet.ErrOnlyPayments, eet.ErrValidation)
	assert.ErrorIs(t, fmt.Errorf("sending: %w", eet.ErrOnlyPayments), eet.ErrOnlyPayments)

	assert.ErrorIs(t, schema, eet.ErrValidation.WithCode("3"))
	assert.NotErrorIs(t, schema, eet.ErrValidation.WithCode("4"))
}
