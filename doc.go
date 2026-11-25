// Package eet records GOBL payments with the Czech EET 2.0 service and
// returns the POK of each; the GOBL addon, with the eet-pok stamp to keep it
// in, lives in the addon subpackage.
//
// The package embeds the time zone database (time/tzdata, about 450 KB) so
// that Europe/Prague always loads, as falling back to UTC would record every
// sale an hour or two off.
package eet
