// Package eet sends GOBL invoices to the Czech Financial Administration's
// EET 2.0 sales-recording service: it builds the Trzba message, signs it
// with WS-Security and reads the Odpoved response. The GOBL addon with the
// EET extensions and rules lives in the addon subpackage.
package eet
