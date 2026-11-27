package eet

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorCause(t *testing.T) {
	err := ErrConnection.withCause(context.DeadlineExceeded)
	assert.EqualError(t, err, "connection: context deadline exceeded")
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assert.Same(t, err, asError(fmt.Errorf("wrapped: %w", err), ErrValidation))
	plain := errors.New("plain")
	assert.ErrorIs(t, asError(plain, ErrValidation), ErrValidation)
}
