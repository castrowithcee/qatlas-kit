// Package web serves the browser interface of an application with its protection built in. The
// application supplies routes and handlers; the server applies the same checks to every request.
//
// Listen binds the server and Serve runs it. By default it binds 127.0.0.1 and ::1.
//
// Events serves a text/event-stream from a Source of the application and resumes it after the
// Last-Event-ID the browser sends back.
//
// Static serves the files of an fs.FS such as an embed.FS for GET and HEAD, mounted as a route like
// "GET /". It reads the files once and answers only for a path that names one of them.
//
// Error answers with a page that names only the status; Redirect sends a browser to another path on this
// server.
//
// # Always enforced
//
// Before a route runs, the server sets a strict Content Security Policy with a fresh nonce per response
// (read with Nonce), nosniff, no referrer and a same-origin resource policy, and grants no CORS access.
// Handlers keep the fixed headers they receive. Static files cannot carry the CSP nonce.
//
// The server refuses a Host header other than localhost, 127.0.0.1 or [::1] with any port, or on the
// network the hosts of the allowed origins, which stops DNS rebinding. An unsafe request from another
// origin is refused through Sec-Fetch-Site and Origin; Origin: null never passes, and a request without
// either header comes from a client that is not a browser and passes. X-Forwarded-* and Forwarded headers
// are removed unless a declared proxy sent them, and a server on the network without TLS serves only a
// declared proxy.
//
// Headers and bodies are bounded in size and time. A long-lived response other than an event stream clears
// its own deadlines with http.ResponseController. Serve waits a short time for running requests when it
// ends and then closes the remaining connections.
//
// Every route pattern names its method, so a handler that changes state is not reached by GET; a pattern
// without a known method, a route without a handler and an invalid or conflicting pattern are refused when
// the server is made. A route that is not Public needs a session: without Sessions such a route is refused,
// unless LoginOff is chosen, which is accepted only on loopback.
//
// An event stream refuses a handshake that comes from another site, and its Origin, if present, must be the
// allowed origin; Origin: null never passes. The route decides about the session as for any route. The
// Last-Event-ID the browser sends back is bounded in length and passed on unchanged. A stream gets a
// heartbeat, a deadline per write, a bounded event size and lifetime, and a bounded number of open streams
// per handler; it ends when the server shuts down. An event ID or type with a line break or NUL ends the
// stream.
//
// Static answers no directory listing, no redirect, no path outside the file system and no file or
// directory whose name starts with a dot. The Content-Type is fixed by extension, application/octet-stream
// for an unknown one, next to the fixed nosniff header. Each file has an ETag of its content with
// Cache-Control: no-cache, so a browser revalidates and receives 304 until the content changes.
//
// Error answers also for unknown paths and methods and for every refusal, and a status outside 400 to 599
// becomes 500. Redirect sends a browser only to a path on this server, with code 301, 302, 303, 307 or 308,
// and refuses any other target with ErrRedirectTarget. Refusals and server errors go to the log with
// request values quoted.
//
// # Options
//
//   - Options.Port: the loopback port. Default: 0, a free port.
//   - Options.Network: binds the server to another address instead of loopback. It starts only with TLS or
//     declared proxies, with the origins browsers use and with Sessions, and Options.Port must stay 0.
//     Default: loopback only, with the host and origin of the loopback address.
//   - Network.Addr: the host:port to bind. It is required with Network.
//   - Network.TLS: serves HTTPS directly. It must hold a certificate or a way to get one; TLS below 1.2 is
//     never used. Default: no TLS, so a network server serves only declared proxies.
//   - Network.Proxies: the addresses of reverse proxies that terminate TLS. Only requests from them may
//     carry X-Forwarded-* and Forwarded headers. Default: no proxy, and those headers are removed.
//   - Network.Origins: the origins browsers use, in the form https://host[:port]. They are required with
//     Network.
//   - Options.Sessions: decides about every route that is not Public. Default: such routes are refused.
//   - Options.Login: LoginOff serves every route without a session and excludes Sessions. Default:
//     LoginRequired, so routes that are not Public need a session.
//   - Route.Public: serves the route without a session. Default: the route needs a session.
//   - Options.Log: receives refusals and the errors of the HTTP server. Default: they are discarded.
//   - Options.Redactor: removes known secrets from every log line. Default: the text is logged as it is.
package web
