# 2. Server-Sent Events for the dashboard, not WebSockets

## Context

The dashboard shows live metric samples, about one per running simulation per second.
The data flows one way only, from the server to the browser. Everything the browser
sends (start a drill, halt it) is an ordinary HTTP request. The API runs on a free Render
instance behind a proxy.

## Decision

Live metrics are served by `GET /api/v1/stream` as Server-Sent Events. The handler writes
`event: metric` frames, sends a `: heartbeat` comment every 15 seconds so proxies keep the
connection open, and flushes and clears the write deadline through
`http.ResponseController`. The browser uses the built in `EventSource`.

## Consequences

- No dependency is needed on either side. It is plain HTTP, so CORS, logging and the
  security headers apply to it like to any other route.
- Middleware must not wrap the `ResponseWriter` in a way that hides flushing. The CORS and
  security header middleware pass the writer through untouched, and tests check this.
- An open stream would block `http.Server.Shutdown` until its timeout. The server
  therefore cancels a stream context through `RegisterOnShutdown`, and the handler links
  it to the request with `context.AfterFunc`.
- The browser reconnects by itself at a fixed pace. The web app closes the source on error
  and reconnects with pauses that double from 1 to 30 seconds, which is kinder to a server
  that is waking up.
- Each browser tab holds one connection open. There is no limit on open streams yet.

## Alternative rejected

WebSockets. They would add a dependency and an upgrade handshake to carry data in a
direction the app never uses. Slack does use a WebSocket (Socket Mode), but that is an
outgoing connection made by the Slack library and has nothing to do with the dashboard.
