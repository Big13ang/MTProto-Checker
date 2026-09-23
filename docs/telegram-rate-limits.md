# Avoiding Telegram API Rate Limits & Throttling

A comprehensive guide explaining how Telegram handles MTProto proxy traffic, what triggers rate limits, how this tool mitigates them, and best practices for large-scale scanning.

---

## 1. Network Topology: Who Does Telegram Actually See?

When checking an MTProto proxy, your computer does **not** communicate directly with Telegram Data Centers (DCs):

```
┌─────────────────┐       Obfuscated2       ┌─────────────────┐       MTProto 2.0       ┌─────────────────┐
│  Checker Client │ ──────────────────────> │  MTProxy Server │ ──────────────────────> │   Telegram DC   │
│   (Your ISP IP) │                         │  (Proxy's IP)   │                         │  (e.g., DC 2)   │
└─────────────────┘                         └─────────────────┘                         └─────────────────┘
```

1. **Your Client IP**: Only visible to the intermediate MTProxy server.
2. **Proxy IP**: Visible to Telegram Data Centers.

Because Telegram sees requests originating from the **proxy's IP address** (not your client IP), testing 100 different proxies concurrently presents to Telegram as 100 distinct worldwide servers establishing connections.

However, rate-limiting can still occur under specific conditions.

---

## 2. The 3 Primary Rate-Limiting Triggers

### A. Diffie-Hellman Key Exchange Storms (The #1 Risk)
Before sending any message, an MTProto client must negotiate an encryption key using a 2048-bit Diffie-Hellman (DH) exchange (`req_pq_multi` $\rightarrow$ `req_DH_params` $\rightarrow$ `set_client_DH_params`).

- **The Danger**: If a script or naive checker creates a new session/auth key for *every single proxy*, Telegram detects thousands of rapid key negotiations from unauthenticated sessions that never engage in chat activity.
- **The Consequence**: Telegram DC triggers temporary IP throttling or `FLOOD_WAIT` on auth key negotiations.

#### Built-in Mitigation in This Codebase:
This tool uses a package-level **shared session storage** (`sharedSession` in `internal/checker/session.go`):
- The **very first** successful check negotiates an auth key with Telegram.
- **All subsequent proxy checks reuse that negotiated auth key**.
- To Telegram, this appears as an existing authenticated client merely resuming sessions across different network routes, **completely bypassing key-generation throttling**.

---

### B. MTProxy Daemon Connection Limits (Proxy-Side Rate Limiting)
Public MTProxy servers run daemons such as `mtprotoproxy` (Python), Erlang MTProxy, or Teleproxy.

- **The Danger**: Proxy administrators frequently configure connection limits (typically **5 to 10 concurrent connections per client IP**) to defend against DoS attacks.
- **The Consequence**: If your scanner attempts 20+ simultaneous connections to the **same proxy host**, that specific proxy daemon will drop or temporarily block your client IP.

#### Mitigation:
- Do not check duplicate links pointing to the same `IP:Port` in parallel.
- Maintain concurrency between **15 and 30** workers per batch.

---

### C. RPC Request Floods (`help.getNearestDC`)
To verify true end-to-end connectivity, the checker calls:
```go
client.API().HelpGetNearestDC(ctx)
```
- `help.getNearestDC` is an unauthenticated, read-only query.
- Sending hundreds of RPC calls per second across the same session can trigger `FLOOD_WAIT_X` from Telegram DCs.

#### Built-in Mitigation in This Codebase:
- **Two-phase filtering**: The fast TCP pre-check discards 80–90% of dead proxies locally without generating any MTProto or RPC traffic to Telegram.
- **Concurrency clamp**: Worker concurrency is strictly bounded (default: `10`, ceiling: `50`).

---

## 3. Recommended Scanning Configuration

| Parameter | Recommended Value | Description |
|---|---|---|
| **Concurrency (`X-Concurrency`)** | `15` – `30` | Optimal balance between scan throughput and staying under proxy-side connection thresholds. |
| **Timeout (`timeout`)** | `4s` – `6s` | Proxies taking longer than 5 seconds to complete handshakes are generally unusable for real messaging. |
| **Continuous Monitoring Pause** | $\ge 20\text{s}$ | When running automated 24/7 monitoring loops across ISPs, wait at least 20–30 seconds between full batch scans. |
| **Deduplication** | Enabled | Ensure input lists are deduplicated by `server:port:secret` prior to scanning. |

---

## 4. Troubleshooting Rate-Limit Symptoms

| Symptom | Probable Cause | Solution |
|---|---|---|
| **All proxies suddenly report `ok: false` for ~5–10 minutes** | Auth key churn or local ISP UDP/TCP throttling | Verify that `sharedSession` is enabled; reduce `X-Concurrency` to `15`. |
| **A specific proxy fails while others succeed** | Proxy administrator per-IP rate limit | Avoid sending parallel requests to that specific proxy host. |
| **`FLOOD_WAIT_X` error in server logs** | Too many RPC calls per second on a single session | Increase sleep intervals between scans or lower batch concurrency. |
