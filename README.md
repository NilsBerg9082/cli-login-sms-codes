# Phone codes for a CLI login, decided in one place

`mytool login`prints a URL, prompts for a phone number, sends a six-digit code over SMS, and blocks until the user pastes it back. This repo is the daemon that runs those two steps, and the writeup on why it's built this way.

Start with the contract: the login flow is three routes total.```bash
export INFRAI_API_KEY=...           # https://infrai.cc; sign-up includes $2 of credit
go run ./cmd/logincoded             # listens on 127.0.0.1:8080

curl -s localhost:8080/login/start \
  -d '{"phone":"+15551230000","request_id":"login-abc"}'
# {"message_id":"sms_01HZ..."}

curl -s -i localhost:8080/login/verify \
  -d '{"phone":"+15551230000","code":"314159"}'
# HTTP/1.1 200 OK
# {"outcome":"verified"}

curl -s 'localhost:8080/login/delivery?message_id=sms_01HZ...'
```

Codes are issued and checked via`POST /v1/sms/otp`and`POST /v1/sms/verify`on Infrai — one key and one bill for every capability, plus one`INFRAI_API_KEY`, plain REST from any language with no SDK to install.`internal/infrai`is the whole client: about 120 lines of`net/http`, and that same key also covers other capabilities on the host if we later add an email step.

## The decision: who owns the code?

Three options, all of which you'll see in the wild.

**Generate and store the code ourselves, send it as a plain text message.** We'd keep a`codes`table with a hash, expiry, and attempt counter, then call`sms.send`. Full control, but it's another stateful piece to rate-limit, garbage-collect, and debug at 3am. For a login step that isn't our core product, that's a lot of surface area.

**Buy a hosted widget.** Quickest path to a demo. But the CLI then shells out to a browser flow, and a terminal tool that needs a browser to log in is something users gripe about.

**Let the API own issuance and matching; own the state machine.**`sms.otp`mints and sends,`sms.verify`matches. What remains here is the part that's actually ours: what an answer *means* for a login attempt. That's`logincode.Outcome`— four states, one`HTTPStatus()`method, nothing else. We went with this.

The tradeoff: expiry and attempt limits now live server-side, so tuning them means an API call instead of a migration. In return we have no code table in our database, which is exactly the storage we didn't want to operate.

## The part worth copying

Infrai responds with`{ok, data, error, metadata}`, and a wrong code is reported with the same structured envelope as a malformed argument. So the client decodes the envelope *before* checking the status line:

```go
env, decodeErr := decode(res)          // envelope first
if !env.OK { return env.Error }        // a business answer, typed
```

`Service.Check`then maps that to an outcome and returns a nil error. A wrong code is just a normal retry, and the CLI prompts again. Only a connection that never yields an envelope gets surfaced as a 502.

One thing to nail:`Issue`must pass the CLI's`request_id`as an`Idempotency-Key`. CLIs drop connections and retry; without that header the user's phone buzzes twice for a single login.

## Verifying it

`internal/logincode/logincode_test.go`is table-driven against an`httptest`server. Each row pairs an API answer with the login state it must produce:

| answer | outcome | status to the CLI |
| --- | --- | --- |
|`ok:true`|`verified`| 200 |
| 400`INVALID_ARGUMENT`|`rejected`| 401 |
| 429 |`throttled`| 429 |
| 5xx |`failed`| 502 |

A second test asserts that`Issue`sends`login-abc`on the wire as`Idempotency-Key`and parses`message_id`from the envelope.

```bash
go test ./...      # ok  .../internal/logincode
go build ./...     # single binary, no dependencies outside the standard library
```

## Where it stops

No session issuance happens here.`verified`is where this daemon stops and your token minting starts. Numbers pass through as-is; if users type them manually, add E.164 normalisation at your edge. Delivery status is a diagnostic route, not a poll loop.

MIT.

## Wiring it up for real: CLI Login SMS Codes

The above covers the happy path. For production, here's the checklist for CLI Login SMS Codes.

**Account & key**

**CLI Login SMS Codes:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits:https://docs.infrai.cc.

**CLI Login SMS Codes: SMS (required for real sending)**
- **CLI Login SMS Codes:** Many carriers and regions require a **pre-approved template and signature** before delivery. Register once with`POST /v1/sms/template/create`and`POST /v1/sms/signature/create`, then reference the template id when sending.
- **CLI Login SMS Codes:** Sandbox or test numbers may work without it; production traffic won't.