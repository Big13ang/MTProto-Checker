package server

import (
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
)

// AllowedHosts is the set of Host values sameOriginOnly accepts, built by
// the caller (cmd/main) from the address actually bound. Empty turns the check
// off — which is both the HOST=0.0.0.0 case and the state every test that does
// not install one runs in.
var AllowedHosts map[string]struct{}

// HostAllowlist returns the Host values a browser can legitimately carry for a
// server bound to addr, or nil when there is nothing to pin.
//
// Only a loopback bind yields a list. A server on 0.0.0.0 or a LAN address is
// reached by names this process cannot know, so any set it invented would refuse
// the real page; that deployment is already the documented no-auth opt-in, where
// anyone routable can drive the endpoints headerless and rebinding buys an
// attacker nothing they could not do directly. Narrowing it further would need
// the operator to name the hosts, which is a config surface nobody has asked for.
//
// All three loopback spellings are listed regardless of which one addr used: the
// browser is opened at the bound address, but a user typing localhost into the
// bar reaches the same server and must not be refused.
//
// The bound address is listed too, and that is not covered by the three: 127/8 is
// loopback in its entirety, so HOST=127.0.0.2 binds, passes shouldOpenBrowser and
// has a browser opened at it -- and without its own entry every POST from that
// page 403s, with the map non-empty so the WARNING line stays silent as well.
// Lowercased because sameOriginOnly lowercases the Host it compares.
//
// The entries carry no port, and the bound one is deliberately not pinned. The
// port in Host is the one the browser was told to connect to, which is not the
// bound one behind `ssh -L 8080:127.0.0.1:3000`, `docker run -p` or any other
// forward, and is absent entirely when the bound port is the scheme default
// (PORT=80). Pinning it refused the real page in all three cases while buying
// nothing: what the check has to refuse is a rebound *name*, and evil.test is
// not a loopback name at any port. The port is still load-bearing in the Origin
// arm below, which compares against the whole of r.Host.
func HostAllowlist(addr string) map[string]struct{} {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil
	}
	allowed := make(map[string]struct{}, 4)
	allowed[strings.ToLower(host)] = struct{}{}
	for _, h := range []string{"127.0.0.1", "localhost", "::1"} {
		allowed[h] = struct{}{}
	}
	return allowed
}

// hostWithoutPort reduces an HTTP Host to the name the allowlist holds: the
// port dropped when there is one, the brackets dropped from an IPv6 literal
// whether or not a port followed it. SplitHostPort is the only thing that can
// tell `[::1]:3000` from `::1`, and it fails on both bracketless and portless
// spellings, so its error is the signal to trim rather than a problem.
func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

// sameOriginOnly refuses a POST that a browser labelled as coming from another
// site. It is a browser control and not authentication — anything that sets its
// own headers walks straight past it, and it must never be counted as a reason
// this server does not need auth.
//
// It exists because every endpoint here acts on its body, and Content-Type:
// text/plain is CORS-safelisted: a plain HTML form on any page the user visits
// reaches them with no preflight and no JavaScript, and the bytes an
// enctype=text/plain form emits are valid JSON, because the = separator lands
// inside a string value. The response stays unreadable cross-origin, but the
// side effect fires and its timing is readable — which turns /fetch-sources'
// caller-supplied socks5.addr into a three-state port-scan oracle over the
// victim's loopback and LAN. That address is left unchecked on the reasoning
// that it is the user's own Tor or tunnel; this is what keeps that true.
//
// Both headers are optional and an absent pair is allowed, deliberately: curl
// and every script against the documented POST /check API send neither, while
// browsers send Sec-Fetch-Site on every request. same-site is rejected along
// with cross-site — a sibling subdomain is not this origin.
//
// The Host check comes first and is not decoration. The Origin comparison below
// reads r.Host, which is whatever the browser was told to ask for, so on its own
// it is satisfiable by DNS rebinding: a page served from evil.test:3000 whose
// name is then repointed at 127.0.0.1 sends Host: evil.test:3000, a matching
// Origin and Sec-Fetch-Site: same-origin. Both arms would pass, and the browser
// would treat the responses as same-origin and let the attacker read them —
// turning /check into an accurate port scanner of the victim's loopback and LAN
// and handing /fetch-sources' bodies back. Pinning Host to the address actually
// bound is what makes the Origin comparison mean anything.
func sameOriginOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(AllowedHosts) > 0 {
			if _, ok := AllowedHosts[strings.ToLower(hostWithoutPort(r.Host))]; !ok {
				jsonResponse(w, http.StatusForbidden, map[string]string{"error": "unexpected Host"})
				return
			}
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
		default:
			jsonResponse(w, http.StatusForbidden, map[string]string{"error": "cross-origin request rejected"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" &&
			origin != "http://"+r.Host && origin != "https://"+r.Host {
			jsonResponse(w, http.StatusForbidden, map[string]string{"error": "cross-origin request rejected"})
			return
		}
		next(w, r)
	}
}

// recoverMiddleware turns a panic in a handler into a 500 with a JSON body,
// so one bad request cannot take the process down mid-scan.
func recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC HTTP %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			}
		}()
		next(w, r)
	}
}
