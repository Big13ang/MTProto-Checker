# 🛡️ MTProto Checker

A powerful, high-performance tool to verify **Telegram MTProto Proxies** by performing real protocol handshakes. Unlike simple TCP checkers, this tool attempts to fetch the actual server configuration from Telegram via the proxy, ensuring 100% connectivity and eliminating the "Connecting..." issue.

## 🌟 Features

* **Deep inspection:** A real MTProto handshake and a `help.getNearestDC` call through the proxy — not a TCP ping — so "working" means Telegram actually connects.
* **Go backend:** Powered by `gotd/td` — fast, stable, one ~18MB binary with zero dependencies.
* **Zero-Redundant DNS & Smart Pipeline:** Reuses resolved IPs and pre-validates secrets in memory to test thousands of proxies in seconds.
* **REST & Streaming API:** Real-time Server-Sent Events (SSE) `/check-stream` endpoint for live scanning, plus `/check` for automated scripts.

## 🚀 Installation

### Build from Source

Requires **Go 1.24+**. [Download Go](https://go.dev/dl/).

```bash
git clone https://github.com/Big13ang/MTProto-Checker.git
cd MTProto-Checker
go build -o mtproto-checker ./cmd/mtproto-checker
./mtproto-checker
```

The server listens on `127.0.0.1:3000` by default. Set `PORT` to change the port, and `HOST=0.0.0.0` to expose it to the network.

## 📚 Documentation

* [API Documentation & Usage Examples](docs/api.md) — Comprehensive guide to endpoints, standardized envelopes, and client code.
* [Avoiding Telegram API Rate Limits](docs/telegram-rate-limits.md) — Deep dive into Telegram rate-limiting mechanisms, shared sessions, and recommended scanning configurations.

## 🔌 Quick API Example

Verify a single proxy:
```bash
curl -X POST http://127.0.0.1:3000/check \
  -H 'Content-Type: application/json' \
  -d '{"server":"1.2.3.4","port":443,"secret":"ee...","timeout":5}'
```

Response:
```json
{
  "success": true,
  "data": {
    "ok": true,
    "ping": 128
  }
}
```

Stream live results for a batch:
```bash
curl -N -X POST http://127.0.0.1:3000/check-stream \
  -H 'Content-Type: application/json' \
  -H 'X-Concurrency: 25' \
  -d '[
    {"server":"1.2.3.4","port":443,"secret":"ee..."},
    {"server":"5.6.7.8","port":8443,"secret":"dd..."}
  ]'
```

## ⚙️ How it Works

1. **Decodes & Validates Secret:** Early in-memory check to reject corrupted or invalid secrets in $0\text{ms}$.
2. **Fast TCP Pre-Check:** Quickly filters out dead endpoints without contacting Telegram DCs.
3. **Connects via MTProto:** Reuses a persistent shared auth key (`sharedSession`) to skip costly 2-second Diffie-Hellman exchanges.
4. **Queries Telegram:** Invokes `help.getNearestDC` to measure authentic round-trip latency.
5. **Result:** Returns confirmed working proxies sorted by response time.

