# strangler-go

Small demo for my FOSDEM 2027 talk in the Go devroom, *Strangler fig in Go: running the old system and the new one side by side*.

The idea is old and simple. You don't rewrite the legacy system in one go, you take out one domain, put a new Go service next to the old code, and a thin router in front decides per route who answers. Like the strangler fig plant, the new code grows around the old system until the old one has nothing left to do.

Everything here is standard library only. No frameworks, no YAML.

## What is inside

There are three small programs:

- `cmd/legacy` is the pretend old system on `:8081`. It has customers and orders, and the habits I saw in almost every legacy system I worked on: `null` instead of an empty list, fields nobody remembers (`legacy_rev`, `_flags`), totals as strings, and "not found" with status 200.
- `cmd/newsvc` is the new customers service on `:8082`. This is the domain we carved out. Same URLs, cleaner answers, and on purpose a bit different, so there is something to find.
- `cmd/router` is the strangler layer on `:8080`, built on `httputil.ReverseProxy`. Each route has a mode in `router.json`:
  - `legacy`: only the old system answers
  - `new`: only the new service, this is after cutover
  - `shadow`: the request goes to both, the client gets the legacy answer, and the new answer is compared in the background

`internal/compare` is the JSON compare. It is the part I spent most of the time on, because for the old clients `null`, `[]` and a missing field are three different answers. Some old clients crash on `null`, some only check if the key exists. So the compare never says they are equal, it tells you which one it was.

## Run it

You need Go 1.22 or newer.

```
make run
```

This builds the three binaries and starts them together. Ctrl+C stops all of them. You can also start them one by one with `make legacy`, `make newsvc` and `make router` in three terminals.

Then try:

```
curl localhost:8080/customers/c-1
curl localhost:8080/customers/c-2
curl "localhost:8080/customers?city=Lleida"
curl -X POST localhost:8080/orders
```

You always get the legacy answer back, look at `"phones":null` for c-2. But the router log shows what the new service would have said:

```
level=DEBUG msg="shadow match" method=GET path=/customers/c-1
level=WARN msg="shadow mismatch" method=GET path=/customers/c-2 count=1 diffs="[$.phones: null vs empty (old=null new=[])]"
level=WARN msg="shadow mismatch" method=GET path="/customers?city=Lleida" count=1 diffs="[$.customers: null vs empty (old=null new=[])]"
```

c-1 matches because `legacy_rev` and `_flags` are in the `ignore` list of `router.json`. c-2 does not, the new service sends `[]` where the old one always sent `null`. Now you have to decide: change the new service to send `null` for a while, or go and check the old clients. Either way you found it in a log line and not in a production incident.

The POST is never shadowed. Only GET and HEAD are, because shadowing a write means the order is created twice and you find out from the customer.

## A few details that matter

- The shadow call has its own deadline (`shadow_timeout`, 300ms by default) and uses `context.WithoutCancel`, so it does not die when the client hangs up, and it can never slow down the real call. There is a test for this with a new service that just hangs.
- Every shadow goroutine ends, even if the new service hangs or legacy is slower than the deadline. The channel between the two sides is buffered, so nobody waits forever on a send. `go test -race` runs this.
- `max_shadow` limits how many shadow calls are in flight. When it is full we skip the shadow, not the real request.
- Status codes are compared too. Try `curl localhost:8080/customers/c-404`: legacy says 200 with an error body, new says 404.
- Bodies bigger than 1MB are served but not compared.

## Config

```json
{
  "listen": ":8080",
  "legacy": "http://localhost:8081",
  "new": "http://localhost:8082",
  "shadow_timeout": "300ms",
  "max_shadow": 100,
  "routes": [
    {"prefix": "/customers", "mode": "shadow"},
    {"prefix": "/orders", "mode": "legacy"}
  ],
  "ignore": ["$.legacy_rev", "$._flags", "$.customers[*].legacy_rev", "$.customers[*]._flags"]
}
```

The longest matching prefix wins, and anything without a route goes to legacy. `/customers` matches `/customers/c-1` but not `/customersfoo`. In `ignore`, `[*]` means any index in a list.

## Tests

```
make test
```

It is `go test -race ./...`. The compare has table tests for the null / empty / missing cases, and the router tests run real HTTP servers with `httptest`.

## What this is not

It is a demo for a 25 minute talk, not a product. There is no database and no auth, and the data is hardcoded. The hard part of a real migration is usually the data ownership, and that part does not fit in a repo like this. I talk about it in the talk.

If you try it and something is not clear, or you have a legacy story with a worse quirk than `null` for empty lists, open an issue. I would like to hear it :))

## License

MIT, see [LICENSE](LICENSE).
