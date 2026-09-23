package checker

import (
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
)

// sharedSession is package-level and shared across all checks on purpose: the
// auth key negotiated by the first successful check is reused by every later
// one, so they skip the DH exchange that otherwise must complete inside the 2s
// ExchangeTimeout. This looks like a bug (mutable state shared across
// goroutines) and was "fixed" once — which took detection from 99/1022 to
// 0/1022. Do not make this per-check again; see the load-bearing rule in
// CLAUDE.md for the measurements.
var sharedSession = &session.StorageMemory{}

// CheckOptionsHook is applied to every check's options just before the client
// is built. It is nil in production and exists for checker_test.go, which
// needs two things no real check may do: trust the fake server's RSA key
// instead of Telegram's, and allow a slower key exchange than the 2s a real
// proxy gets, because the fake server's DH work runs on the same CPU as the
// test. Do not reach for it to change SessionStorage — see the load-bearing
// rule on sharedSession.
var CheckOptionsHook func(*telegram.Options)

// newCheckOptions returns client options for one proxy check. All checks share
// sharedSession deliberately — a real Telegram client also reuses its auth key
// rather than running a fresh key exchange per connection.
func newCheckOptions(resolver dcs.Resolver) telegram.Options {
	opts := telegram.Options{
		Resolver:        resolver,
		SessionStorage:  sharedSession,
		DialTimeout:     minTimeoutDuration,
		ExchangeTimeout: 2 * time.Second,
		NoUpdates:       true,
		Device:          telegram.DeviceTDesktopWindows(),
	}
	if CheckOptionsHook != nil {
		CheckOptionsHook(&opts)
	}
	return opts
}
