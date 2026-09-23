package server

import "time"

const (
	// maxBatchSize entries at ~500 B of worst-case JSON each is ~5 MiB;
	// maxBodySize leaves headroom above that. Exceeding either → 413.
	maxBodySize  = 8 * 1024 * 1024
	maxBatchSize = 10_000
	// A public proxy list is tens of KiB; the whole 17-source corpus is
	// ~233 KB. 1 MiB per source is far above anything plausible and is
	// enforced while reading, so an endless response is cut off, not buffered.
	//
	// maxSourceBytes alone bounds nothing useful: /fetch-sources buffers every
	// source whole until the last one lands, so the per-request ceiling is what
	// actually caps resident memory. maxSources sources at maxSourceBytes each
	// would be 20 MiB per request with nothing limiting how many requests run at
	// once, so the two limits are enforced together — maxRequestSourceBytes is a
	// budget the sources of one request share, and maxFetchSourcesInFlight caps
	// the number of requests holding one.
	maxSourceBytes        = 1024 * 1024
	maxRequestSourceBytes = 4 * 1024 * 1024
	// Every cap above is enforced on the response body. Headers are read and
	// parsed into the header map before the body is touched, so they escape all
	// of them: at the stdlib default of 10 MiB a source whose body is 2 bytes
	// still costs ~11.6 MiB resident, measured — 20 sources across 4 requests
	// in flight is ~880 MiB against the 16 MiB this design intends. A proxy
	// list's headers are a few hundred bytes; 64 KiB is far above any of them.
	maxSourceHeaderBytes    = 64 * 1024
	maxFetchSourcesInFlight = 4
	maxSources              = 20
	maxConcurrency          = 50

	// ShutdownTimeout is exported for cmd/main to use.
	ShutdownTimeout = 5 * time.Second
)

// SOCKS5Config is the optional proxy a /fetch-sources request may carry. An
// absent config — or an empty address — means direct-only.
type SOCKS5Config struct {
	Addr string `json:"addr"`
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// FetchSourcesRequest is the JSON body for POST /fetch-sources.
type FetchSourcesRequest struct {
	URLs   []string      `json:"urls"`
	SOCKS5 *SOCKS5Config `json:"socks5,omitempty"`
}

// CheckRequest is the JSON body for POST /check and elements of
// POST /check-batch and /check-stream arrays.
type CheckRequest struct {
	Server  string `json:"server"`
	Port    int    `json:"port"`
	Secret  string `json:"secret"`
	Timeout int    `json:"timeout,omitempty"`
}

// CheckResponse is the JSON response for a single proxy check.
type CheckResponse struct {
	OK   bool  `json:"ok"`
	Ping int64 `json:"ping,omitempty"`
}
