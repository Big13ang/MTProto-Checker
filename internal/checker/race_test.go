//go:build race

package checker

// raceTimeoutFactor stretches the wall-clock budgets in checker_test.go when
// the binary is built with the race detector. The instrumented handshake against
// the fake proxy does its RSA and DH work on the same CPU as the client, and on a
// loaded two-core CI runner that ran past the 20s budget often enough to make the
// race step a coin flip — with no DATA RACE reported, i.e. pure overhead.
//
// It is a factor rather than a larger constant everywhere so the ordinary run
// keeps asserting the tight budget, which is the one that would catch a real
// regression in how long a check takes.
const raceTimeoutFactor = 8
