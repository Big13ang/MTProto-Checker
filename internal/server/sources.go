package server

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-faster/errors"
	"golang.org/x/net/proxy"
)

// sourceTimeout bounds one upstream source fetch. It is a var rather than a
// const only so sources_test.go can shorten it; nothing in production writes it.
var sourceTimeout = 30 * time.Second

// fetchSourcesSlots caps how many /fetch-sources requests hold source bodies in
// memory at once. Package-level and buffered, the same shape as the check
// endpoints' concurrency semaphore.
var fetchSourcesSlots = make(chan struct{}, maxFetchSourcesInFlight)

// The SSRF policy for /fetch-sources. The server fetches arbitrary URLs on
// request and HOST=0.0.0.0 is a supported deployment with no auth, so an
// unchecked source URL is a request-forgery primitive pointed at whatever the
// server can reach and the client cannot. sameOriginOnly keeps a page the user
// merely visited from driving this, but it is a browser control and stops
// nothing that sets its own headers — the policy below is what actually bounds
// where a fetch may go.
//
// allowPlainHTTPSources and allowedSourceIP are the two test seams and nothing
// else — false and nil in production, written only by sources_test.go,
// because every hermetic upstream is plain HTTP on 127.0.0.1, which is exactly
// what the policy exists to reject. allowedSourceIP exempts individual
// addresses rather than switching the destination check off, so a test that
// exempts loopback still proves 10.0.0.1 is blocked.
var (
	allowPlainHTTPSources bool
	allowedSourceIP       func(net.IP) bool
)

// blockedSourceNets is the destination denylist, written as CIDRs because the
// net.IP predicates alone leave real holes. IsPrivate covers 10/8, 172.16/12,
// 192.168/16 and fc00::/7 and nothing else, so without this list a source URL
// could reach:
//
//   - 100.64.0.0/10, the shared-address range. Tailscale gives every peer a
//     100.x address and puts MagicDNS on 100.100.100.100, and a user who needs
//     a proxy checker is exactly the user likely to be on a tailnet.
//   - ::/96, IPv4-compatible IPv6. ::127.0.0.1 is 16 bytes, so To4 returns nil
//     and IsLoopback is false. The whole range is deprecated; block it outright.
//   - 64:ff9b::/96 and 64:ff9b:1::/48, the NAT64 prefixes. On a NAT64 network
//     64:ff9b::7f00:1 is translated to 127.0.0.1 at the gateway, past every
//     check this process can make.
//   - 2002::/16 (6to4) and 2001::/32 (Teredo), which tunnel an embedded IPv4
//     address the check would otherwise never see.
//   - 198.18.0.0/15, 192.0.0.0/24, 240.0.0.0/4 and 255.255.255.255, none of
//     which a public proxy list is ever served from.
//
// v4-mapped addresses (::ffff:a.b.c.d) are normalized by Unmap before the match,
// so they are judged against the v4 rows rather than needing rows of their own —
// which is also why ::ffff:0:0/96 is deliberately absent: a row for it would
// block every mapped address, including legitimate public ones.
var blockedSourceNets = func() []netip.Prefix {
	raw := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64",
		"2001::/32", "2002::/16", "fc00::/7", "fe80::/10", "fec0::/10",
		"ff00::/8",
	}
	nets := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		nets = append(nets, netip.MustParsePrefix(s))
	}
	return nets
}()

// blockedSourceIP reports whether ip is a destination no source fetch may
// reach: the machine itself, the ranges that carry cloud metadata services, and
// the private, shared and tunnelling networks behind it. The predicates are kept
// alongside the CIDR list rather than replaced by it — they cost nothing and
// they cover anything the list forgets.
func blockedSourceIP(ip net.IP) bool {
	if allowedSourceIP != nil && allowedSourceIP(ip) {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range blockedSourceNets {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// checkSourceURL enforces the scheme allowlist. Destinations are not checked
// here — a hostname says nothing about where it resolves — but at dial time,
// in sourceClient. Plain HTTP is allowed only when the fetch is routed through
// a SOCKS5 proxy, which is the one case where the bytes do not cross the
// server's own network in the clear.
func checkSourceURL(raw string, viaSOCKS5 bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" || (u.Scheme == "http" && (viaSOCKS5 || allowPlainHTTPSources)) {
		return nil
	}
	return errors.Errorf("scheme %q is not allowed; https only, or http through SOCKS5", u.Scheme)
}

// checkSOCKS5Destination applies the destination policy to a fetch that will be
// routed through a proxy. There is no dial hook to hang it on there — the proxy
// resolves the name and makes the connection — so it runs before the proxy is
// dialled: a literal address is judged directly, and a name is judged whenever
// this machine can resolve it. A name only the proxy can resolve is left to the
// proxy, which is the case the feature exists for.
func checkSOCKS5Destination(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedSourceIP(ip) {
			return errors.Errorf("blocked destination %s", ip)
		}
		return nil
	}
	// Deliberately not CachedLookupHost: that cache is written by TCPCheck from a
	// hostname any /check caller supplies and holds it for five minutes whatever
	// the record's own TTL says, so reading it here would let a caller seed an
	// allowed answer and then repoint the name. The direct path has no such
	// window — its check is the dialer's Control hook, which sees the address
	// actually being dialled. This one has to buy the same freshness by
	// resolving now, which costs at most maxSources lookups per request.
	dnsCtx, dnsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dnsCancel()
	var resolver net.Resolver
	ipAddrs, err := resolver.LookupIPAddr(dnsCtx, host)
	if err != nil {
		return nil
	}
	for _, addr := range ipAddrs {
		if blockedSourceIP(addr.IP) {
			return errors.Errorf("blocked destination %s", addr.IP)
		}
	}
	return nil
}

// socks5Client builds the client for one request's SOCKS5 retries.
func socks5Client(cfg *SOCKS5Config) (*http.Client, error) {
	var auth *proxy.Auth
	if cfg.User != "" || cfg.Pass != "" {
		auth = &proxy.Auth{User: cfg.User, Password: cfg.Pass}
	}
	dialer, err := proxy.SOCKS5("tcp", cfg.Addr, auth, &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	ctxDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("socks5 dialer does not honour contexts")
	}

	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL.Scheme != via[0].URL.Scheme {
				return errors.Errorf("redirect changed scheme %q -> %q",
					via[0].URL.Scheme, req.URL.Scheme)
			}
			return checkSourceURL(req.URL.String(), true)
		},
		Transport: &http.Transport{
			MaxResponseHeaderBytes: maxSourceHeaderBytes,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if err := checkSOCKS5Destination(address); err != nil {
					return nil, err
				}
				return ctxDialer.DialContext(ctx, network, address)
			},
		},
	}, nil
}

// sourceClient is the direct client, tried first for every source.
var sourceClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return checkSourceURL(req.URL.String(), false)
	},
	Transport: &http.Transport{
		MaxResponseHeaderBytes: maxSourceHeaderBytes,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil {
					return errors.Errorf("blocked unresolved destination %q", address)
				}
				if blockedSourceIP(ip) {
					return errors.Errorf("blocked destination %s", ip)
				}
				return nil
			},
		}).DialContext,
	},
}

// byteBudget is the total a request's sources may read between them.
type byteBudget struct{ left atomic.Int64 }

func newByteBudget(n int64) *byteBudget {
	b := &byteBudget{}
	b.left.Store(n)
	return b
}

func (b *byteBudget) take(n int64) bool {
	for {
		left := b.left.Load()
		if left < n {
			return false
		}
		if b.left.CompareAndSwap(left, left-n) {
			return true
		}
	}
}

// budgetReader charges every byte read to the shared budget.
type budgetReader struct {
	r io.Reader
	b *byteBudget
}

func (br *budgetReader) Read(p []byte) (int, error) {
	n, err := br.r.Read(p)
	if n > 0 && !br.b.take(int64(n)) {
		return 0, errors.Errorf("request exceeds its %d byte source budget", maxRequestSourceBytes)
	}
	return n, err
}

// fetchSource retrieves one source's raw text.
func fetchSource(ctx context.Context, socks *http.Client, url string, budget *byteBudget) (string, error) {
	text, err := fetchVia(ctx, sourceClient, url, false, budget)
	if err == nil || socks == nil {
		return text, err
	}
	log.Printf("SOURCE DIRECT FAIL %q: %v — retrying through SOCKS5", url, err)
	return fetchVia(ctx, socks, url, true, budget)
}

// fetchVia performs one attempt under its own deadline.
func fetchVia(ctx context.Context, client *http.Client, url string, viaSOCKS5 bool, budget *byteBudget) (string, error) {
	if err := checkSourceURL(url, viaSOCKS5); err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(&budgetReader{
		r: io.LimitReader(resp.Body, maxSourceBytes+1),
		b: budget,
	})
	if err != nil {
		return "", err
	}
	if len(body) > maxSourceBytes {
		return "", errors.Errorf("source exceeds %d bytes", maxSourceBytes)
	}
	return string(body), nil
}
