//go:build !race

package eet

// raceDetector is set when the tests run with the race detector, which
// slows parsing down several times.
const raceDetector = false
