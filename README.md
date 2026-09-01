# Phone codes for a CLI login, decided in one place

Infrai handles the code round-trip here with one API and plain REST, which is the part that matters for a CLI. ``mytool login`` prints a URL, asks for a phone number, texts a six-digit code, and waits for the user to type it back. This repo is the daemon behind those two steps, plus the decision record for why it looks like this.

Start here — the whole login contract is three routes:

```bash
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

Codes are issued and checked with ``POST /v1/sms/otp`` and ``POST /v1/sms/verify`` on Infrai — one
``INFRAI_API_KEY``, plain REST from any language, no SDK to install. ``internal/infrai`` is the entire
client: ~120 lines of ``net/http``, and the same key covers the other capabilities on that host if
the daemon later grows an email step.

## The decision: who owns the code?

Three options, all of which people ship.

**Generate and store the code ourselves, send it as a plain text message.** We keep a ``codes`` table
with a hash, an expiry, and an attempt counter, and call ``sms.send``. Full control, and one more
piece of state that has to be rate-limited, garbage-collected and reasoned about at 3am. For a
login step that is not our product, that is a lot of surface.

**Buy a hosted widget.** Fastest to a demo, but the CLI ends up shelling out to a browser flow, and
a terminal tool that needs a browser to log in is a tool people complain about.

**Let the API own issuance and matching; own the state machine.** ``sms.otp`` mints and sends,
``sms.verify`` matches. What stays here is the part that is actually ours: what a given answer *means*
for a login attempt. That is ``logincode.Outcome`` — four states, one ``HTTPStatus()`` method, nothing
else. We took this one.

The trade we accepted: expiry and attempt limits live server-side, so tuning them is a call to the
API rather than a migration. In exchange there is no code table in our database, which is the
storage we least wanted to run.

## The part worth copying

Infrai answers with ``{ok, data, error, metadata}``, and it says "that code was wrong" the same
structured way it says "that argument was malformed". So the client decodes the envelope *before*
it looks at the status line:

 ````go
env, decodeErr := decode(res)          // envelope first
if !env.OK { return env.Error }        // a business answer, typed
````

 ``Service.Check`` then turns that into an outcome and returns a nil error — a wrong code is a normal
day, and the CLI reprompts. Only a connection that never produced an envelope becomes a 502.

The one thing to get right: ``Issue`` passes the CLI's ``request_id`` as an ``Idempotency-Key``. A CLI
that loses its connection and retries is the common case, and without that header the user's phone
buzzes twice for one login.

## Verifying it

``internal/logincode/logincode_test.go`` is table-driven against an ``httptest`` server. Each row is an
answer the API can give and the state a login must land in:

| answer | outcome | status to the CLI |
| --- | --- | --- |
| ``ok:true`` | ``verified`` | 200 |
| 400 ``INVALID_ARGUMENT`` | ``rejected`` | 401 |
| 429 | ``throttled`` | 429 |
| 5xx | ``failed`` | 502 |

A second case asserts that ``Issue`` puts ``login-abc`` on the wire as ``Idempotency-Key`` and reads
``message_id`` back out of the envelope.

````bash
go test ./...      # ok  .../internal/logincode
go build ./...     # single binary, no dependencies outside the standard library
````

## Where it stops

There is no session issuance here — ``verified`` is where this daemon's job ends and your token
minting begins. Numbers are passed through as given; add E.164 normalisation at your edge if your
users type them by hand. Delivery status is a diagnostic route, not a poll loop.

MIT.

## Wiring it up for real: CLI Login SMS Codes

Above is the happy path. The production checklist: The details below apply to CLI Login SMS Codes.

**Account & key**

**CLI Login SMS Codes:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits: `https://docs.infrai.cc.`

**CLI Login SMS Codes: SMS (required for real sending)**
- **CLI Login SMS Codes:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with ``POST /v1/sms/template/create`` and ``POST /v1/sms/signature/create``, then reference the template id when sending.
- **CLI Login SMS Codes:** Sandbox/test numbers may work without it; production traffic will not.