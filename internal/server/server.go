package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-faster/errors"
	"github.com/rahgozar94725/MTProto-Checker/internal/checker"
)

// readCheckRequests decodes a batch request body, enforcing maxBodySize and
// maxBatchSize. On failure it returns a non-zero HTTP status and a message the
// caller should send as {"error": msg}; on success status is 0.
func readCheckRequests(w http.ResponseWriter, r *http.Request) ([]CheckRequest, int, string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var reqs []CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body exceeds %d bytes", maxBodySize)
		}
		return nil, http.StatusBadRequest, "invalid JSON"
	}
	if len(reqs) > maxBatchSize {
		return nil, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("too many proxies: %d, max %d per request", len(reqs), maxBatchSize)
	}
	return reqs, 0, ""
}

// concurrencyLimit reads X-Concurrency and clamps it to what the two batch
// endpoints' semaphores will honour.
func concurrencyLimit(h http.Header) int {
	limit := 10
	if l := h.Get("X-Concurrency"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxConcurrency {
		limit = maxConcurrency
	}
	return limit
}

// NewMux wires all endpoints. Split out so tests can drive them with httptest
// instead of a live listener; the caller adds nothing but the server, signals
// and browser launch.
func NewMux(version string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/check", recoverMiddleware(sameOriginOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var req CheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				jsonResponse(w, http.StatusRequestEntityTooLarge,
					map[string]string{"error": fmt.Sprintf("request body exceeds %d bytes", maxBodySize)})
				return
			}
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		timeout := req.Timeout
		if timeout < checker.MinTimeout || timeout > checker.MaxTimeout {
			timeout = checker.DefaultTimeout
		}

		start := time.Now()
		ping, err := checker.CheckProxy(r.Context(), req.Server, req.Port, req.Secret, timeout)
		elapsed := time.Since(start)

		if err != nil {
			log.Printf("CHECK FAIL %s:%d timeout=%ds (%v)", req.Server, req.Port, timeout, elapsed)
			jsonResponse(w, http.StatusOK, CheckResponse{OK: false})
		} else {
			log.Printf("CHECK OK   %s:%d %dms timeout=%ds (%v)", req.Server, req.Port, ping, timeout, elapsed)
			jsonResponse(w, http.StatusOK, CheckResponse{OK: true, Ping: ping})
		}
	})))

	mux.HandleFunc("/check-batch", recoverMiddleware(sameOriginOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", `</check>; rel="alternate", </check-stream>; rel="successor-version"`)
		log.Printf("DEPRECATED /check-batch hit from %s — use /check for scripting or /check-stream for streaming; removal planned in a future release", r.RemoteAddr)

		reqs, status, msg := readCheckRequests(w, r)
		if status != 0 {
			jsonResponse(w, status, map[string]string{"error": msg})
			return
		}

		limit := concurrencyLimit(r.Header)

		timeout := checker.DefaultTimeout
		if len(reqs) > 0 && reqs[0].Timeout >= checker.MinTimeout && reqs[0].Timeout <= checker.MaxTimeout {
			timeout = reqs[0].Timeout
		}

		log.Printf("BATCH START %d proxies, concurrency=%d, timeout=%ds", len(reqs), limit, timeout)
		start := time.Now()

		results := make([]CheckResponse, len(reqs))

		type indexedReq struct {
			idx int
			req CheckRequest
		}

		// Phase 1: TCP pre-check — filter dead proxies fast (~3s max)
		tcpStart := time.Now()
		var reachable []indexedReq
		var reachableMu sync.Mutex
		var tcpWg sync.WaitGroup
		tcpSem := make(chan struct{}, limit)

		for i, p := range reqs {
			tcpWg.Add(1)
			go func(idx int, proxy CheckRequest) {
				defer tcpWg.Done()
				tcpSem <- struct{}{}
				defer func() { <-tcpSem }()

				if err := checker.TCPCheck(proxy.Server, proxy.Port); err != nil {
					results[idx] = CheckResponse{OK: false}
				} else {
					reachableMu.Lock()
					reachable = append(reachable, indexedReq{idx: idx, req: proxy})
					reachableMu.Unlock()
				}
			}(i, p)
		}
		tcpWg.Wait()
		log.Printf("TCP phase done: %d/%d reachable (%v)", len(reachable), len(reqs), time.Since(tcpStart))

		// Phase 2: Full Telegram check — only for reachable proxies
		telegramStart := time.Now()
		telegramSem := make(chan struct{}, limit)
		var telegramWg sync.WaitGroup

		for _, ir := range reachable {
			telegramWg.Add(1)
			go func(item indexedReq) {
				defer telegramWg.Done()
				telegramSem <- struct{}{}
				defer func() { <-telegramSem }()

				t := item.req.Timeout
				if t < checker.MinTimeout || t > checker.MaxTimeout {
					t = checker.DefaultTimeout
				}
				ping, err := checker.CheckProxy(r.Context(), item.req.Server, item.req.Port, item.req.Secret, t)
				if err != nil {
					results[item.idx] = CheckResponse{OK: false}
				} else {
					results[item.idx] = CheckResponse{OK: true, Ping: ping}
				}
			}(ir)
		}
		telegramWg.Wait()

		working := 0
		for _, res := range results {
			if res.OK {
				working++
			}
		}
		log.Printf("BATCH DONE  %d/%d working | tcp=%v telegram=%v total=%v",
			working, len(reqs), time.Since(tcpStart), time.Since(telegramStart), time.Since(start))

		jsonResponse(w, http.StatusOK, results)
	})))

	mux.HandleFunc("/check-stream", recoverMiddleware(sameOriginOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}

		reqs, status, msg := readCheckRequests(w, r)
		if status != 0 {
			jsonResponse(w, status, map[string]string{"error": msg})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		limit := concurrencyLimit(r.Header)

		timeout := checker.DefaultTimeout
		if len(reqs) > 0 && reqs[0].Timeout >= checker.MinTimeout && reqs[0].Timeout <= checker.MaxTimeout {
			timeout = reqs[0].Timeout
		}

		total := len(reqs)
		log.Printf("STREAM START %d proxies, concurrency=%d, timeout=%ds", total, limit, timeout)

		type strProgress struct {
			Completed int    `json:"completed"`
			Total     int    `json:"total"`
			Working   int    `json:"working"`
			Server    string `json:"server"`
			Port      int    `json:"port"`
			Secret    string `json:"secret"`
			OK        bool   `json:"ok"`
			Ping      int64  `json:"ping,omitempty"`
		}

		sendEvent := func(event string, v interface{}) {
			data, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		sendEvent("progress", &strProgress{Completed: 0, Total: total, Working: 0})

		sem := make(chan struct{}, limit)
		var mu sync.Mutex
		var wg sync.WaitGroup

		completed := 0
		working := 0

		for _, p := range reqs {
			wg.Add(1)
			go func(proxy CheckRequest) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				t := proxy.Timeout
				if t < checker.MinTimeout || t > checker.MaxTimeout {
					t = timeout
				}

				err := checker.TCPCheck(proxy.Server, proxy.Port)
				if err != nil {
					mu.Lock()
					completed++
					sendEvent("progress", &strProgress{
						Completed: completed, Total: total, Working: working,
						Server: proxy.Server, Port: proxy.Port, Secret: proxy.Secret,
						OK: false,
					})
					mu.Unlock()
					return
				}

				hardCtx, hardCancel := context.WithTimeout(r.Context(), time.Duration(t+10)*time.Second)
				defer hardCancel()

				type tgResult struct {
					ping int64
					err  error
				}
				tgCh := make(chan tgResult, 1)
				go func() {
					ping, tgErr := checker.CheckProxy(hardCtx, proxy.Server, proxy.Port, proxy.Secret, t)
					tgCh <- tgResult{ping, tgErr}
				}()

				var ping int64
				var tgErr error
				select {
				case res := <-tgCh:
					ping = res.ping
					tgErr = res.err
				case <-hardCtx.Done():
					tgErr = hardCtx.Err()
				}

				mu.Lock()
				completed++
				if tgErr != nil {
					sendEvent("progress", &strProgress{
						Completed: completed, Total: total, Working: working,
						Server: proxy.Server, Port: proxy.Port, Secret: proxy.Secret,
						OK: false,
					})
				} else {
					working++
					sendEvent("progress", &strProgress{
						Completed: completed, Total: total, Working: working,
						Server: proxy.Server, Port: proxy.Port, Secret: proxy.Secret,
						OK: true, Ping: ping,
					})
				}
				mu.Unlock()
			}(p)
		}

		wg.Wait()
		log.Printf("STREAM DONE %d/%d working", working, total)
		sendEvent("done", map[string]int{"working": working, "total": total})
	})))

	mux.HandleFunc("/fetch-sources", recoverMiddleware(sameOriginOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		select {
		case fetchSourcesSlots <- struct{}{}:
			defer func() { <-fetchSourcesSlots }()
		case <-r.Context().Done():
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var req FetchSourcesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				jsonResponse(w, http.StatusRequestEntityTooLarge,
					map[string]string{"error": fmt.Sprintf("request body exceeds %d bytes", maxBodySize)})
				return
			}
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if len(req.URLs) > maxSources {
			jsonResponse(w, http.StatusRequestEntityTooLarge,
				map[string]string{"error": fmt.Sprintf("too many sources: %d, max %d per request", len(req.URLs), maxSources)})
			return
		}

		var socks *http.Client
		if req.SOCKS5 != nil && req.SOCKS5.Addr != "" {
			client, err := socks5Client(req.SOCKS5)
			if err != nil {
				jsonResponse(w, http.StatusBadRequest,
					map[string]string{"error": fmt.Sprintf("invalid socks5 proxy: %v", err)})
				return
			}
			socks = client
			defer socks.CloseIdleConnections()
		}

		texts := make([]string, len(req.URLs))
		budget := newByteBudget(maxRequestSourceBytes)
		var wg sync.WaitGroup
		for i, u := range req.URLs {
			wg.Add(1)
			go func(idx int, url string) {
				defer wg.Done()
				text, err := fetchSource(r.Context(), socks, url, budget)
				if err != nil {
					log.Printf("SOURCE FAIL %q: %v", url, err)
					return
				}
				texts[idx] = text
			}(i, u)
		}
		wg.Wait()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, text := range texts {
			if text == "" {
				continue
			}
			io.WriteString(w, text)
			if !strings.HasSuffix(text, "\n") {
				io.WriteString(w, "\n")
			}
		}
	})))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			jsonResponse(w, http.StatusNotFound, map[string]string{"error": "Resource not found"})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{
			"service": "mtproto-checker",
			"status":  "ok",
			"version": version,
		})
	})

	return mux
}
