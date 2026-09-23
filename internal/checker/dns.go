package checker

import (
	"context"
	"net"
	"sync"
	"time"
)

type dnsCacheEntry struct {
	ips  []net.IP
	next time.Time
}

var (
	dnsCacheMu sync.RWMutex
	dnsCache   = make(map[string]*dnsCacheEntry)
)

// CachedLookupHost resolves a hostname with a 5-minute cache to avoid
// repeated DNS lookups for the same proxy host.
func CachedLookupHost(host string) ([]net.IP, error) {
	dnsCacheMu.RLock()
	entry, ok := dnsCache[host]
	dnsCacheMu.RUnlock()
	if ok && time.Now().Before(entry.next) {
		return entry.ips, nil
	}

	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	dnsCtx, dnsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dnsCancel()
	var resolver net.Resolver
	ipAddrs, err := resolver.LookupIPAddr(dnsCtx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(ipAddrs))
	for i, a := range ipAddrs {
		ips[i] = a.IP
	}

	dnsCacheMu.Lock()
	dnsCache[host] = &dnsCacheEntry{ips: ips, next: time.Now().Add(5 * time.Minute)}
	dnsCacheMu.Unlock()
	return ips, nil
}

// SeedDNSCache injects a DNS cache entry and returns a cleanup function that
// restores the previous state. Exported for cross-package tests (e.g.
// internal/server tests that need to pre-seed DNS to avoid real lookups).
func SeedDNSCache(host string, ip net.IP) func() {
	dnsCacheMu.Lock()
	orig, had := dnsCache[host]
	dnsCache[host] = &dnsCacheEntry{
		ips:  []net.IP{ip},
		next: time.Now().Add(5 * time.Minute),
	}
	dnsCacheMu.Unlock()
	return func() {
		dnsCacheMu.Lock()
		if had {
			dnsCache[host] = orig
		} else {
			delete(dnsCache, host)
		}
		dnsCacheMu.Unlock()
	}
}
