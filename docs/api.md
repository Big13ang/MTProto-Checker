# MTProto Checker API Documentation

All API responses strictly adhere to a standardized JSON envelope pattern.

---

## 1. Response Envelope Conventions

### Success Envelope (`200 OK`)
All successful HTTP responses are wrapped in a standard structure:

```json
{
  "success": true,
  "data": { ... }
}
```

### Error Envelope (`4xx / 5xx`)
All error responses return `success: false` and a typed `error` object with standard error codes:

```json
{
  "success": false,
  "error": {
    "code": "BAD_REQUEST",
    "message": "request body exceeds 8388608 bytes"
  }
}
```

#### Standard Error Codes:
| HTTP Status | Error Code | Description |
|---|---|---|
| `400` | `BAD_REQUEST` | Malformed JSON or invalid parameter syntax |
| `401` | `UNAUTHORIZED` | Missing or invalid authentication token |
| `403` | `FORBIDDEN` | Cross-Origin request rejected by origin guard |
| `404` | `NOT_FOUND` | Endpoint does not exist |
| `405` | `METHOD_NOT_ALLOWED` | Wrong HTTP method (e.g. GET on POST endpoint) |
| `413` | `PAYLOAD_TOO_LARGE` | Body exceeds 8MB or proxy count > 10,000 |
| `500` | `INTERNAL_SERVER_ERROR`| Unhandled server error |

---

## 2. Endpoints Reference

### 2.1 Health / Root (`GET /`)
Quick status check.

#### Request:
```bash
curl http://127.0.0.1:3000/
```

#### Response (`200 OK`):
```json
{
  "success": true,
  "data": {
    "service": "MTProto Checker",
    "version": "dev",
    "status": "running"
  }
}
```

---

### 2.2 Check Single Proxy (`POST /check`)
Performs a full Telegram MTProto handshake against a single proxy and measures ping.

#### Request:
```bash
curl -X POST http://127.0.0.1:3000/check \
  -H "Content-Type: application/json" \
  -d '{
    "server": "ir.macauley.info",
    "port": 8443,
    "secret": "EERighJJvXrFGRMCIMjdCQ",
    "timeout": 5
  }'
```

#### Fields:
- `server` (*string, required*): Domain or IP of the proxy.
- `port` (*int, required*): Port number (1-65535).
- `secret` (*string, required*): Hex or Base64 MTProxy secret.
- `timeout` (*int, optional*): Seconds before timing out (min: 3, max: 30, default: 5).

#### Response (`200 OK`):
- **Working Proxy**:
  ```json
  {
    "success": true,
    "data": {
      "ok": true,
      "ping": 132
    }
  }
  ```
- **Dead / Blocked Proxy**:
  ```json
  {
    "success": true,
    "data": {
      "ok": false
    }
  }
  ```

---

### 2.3 Real-Time Streaming Batch (`POST /check-stream`)
Streams real-time progress events as each proxy finishes testing.

#### Headers:
- `X-Concurrency` (*int, optional*): Number of parallel test workers (1 to 50, default: 10).

#### Request:
```bash
curl -N -X POST http://127.0.0.1:3000/check-stream \
  -H "Content-Type: application/json" \
  -H "X-Concurrency: 25" \
  -d '[
    {"server": "149.154.167.50", "port": 443, "secret": "ee00000000000000000000000000000000"},
    {"server": "ir.macauley.info", "port": 8443, "secret": "EERighJJvXrFGRMCIMjdCQ"}
  ]'
```

#### Stream Output (`text/event-stream`):

```text
event: progress
data: {"completed":0,"total":2,"working":0}

event: progress
data: {"completed":1,"total":2,"working":1,"server":"ir.macauley.info","port":8443,"secret":"EERighJJvXrFGRMCIMjdCQ","ok":true,"ping":132}

event: progress
data: {"completed":2,"total":2,"working":1,"server":"149.154.167.50","port":443,"secret":"ee00000000000000000000000000000000","ok":false}

event: done
data: {"completed":2,"total":2,"working":1}
```

---

### 2.4 Synchronous Batch (`POST /check-batch`)
Tests an array of proxies and returns all results together after completion.

#### Request:
```bash
curl -X POST http://127.0.0.1:3000/check-batch \
  -H "Content-Type: application/json" \
  -H "X-Concurrency: 20" \
  -d '[
    {"server": "ir.macauley.info", "port": 8443, "secret": "EERighJJvXrFGRMCIMjdCQ", "timeout": 5}
  ]'
```

#### Response (`200 OK`):
```json
{
  "success": true,
  "data": [
    {
      "ok": true,
      "ping": 132
    }
  ]
}
```

---

### 2.5 Fetch Upstream Sources (`POST /fetch-sources`)
Fetches proxy lists from remote URLs with SSRF mitigation and optional SOCKS5 proxying.

#### Request:
```bash
curl -X POST http://127.0.0.1:3000/fetch-sources \
  -H "Content-Type: application/json" \
  -d '{
    "urls": [
      "https://raw.githubusercontent.com/Argh94/Proxy-List/main/MTProto.txt"
    ]
  }'
```

#### Response (`200 OK`):
```json
{
  "success": true,
  "data": {
    "sources": [
      {
        "url": "https://raw.githubusercontent.com/Argh94/Proxy-List/main/MTProto.txt",
        "body": "tg://proxy?server=...",
        "error": ""
      }
    ]
  }
}
```
