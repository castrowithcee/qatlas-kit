// Package web serves the browser interface of an application with its protection built in. The
// application supplies routes and handlers; the server applies the same checks to every request.
//
// Listen binds the server and Serve runs it. By default it binds 127.0.0.1 and ::1.
//
// Events serves a text/event-stream from a Source of the application and resumes it after the
// Last-Event-ID the browser sends back.
//
// WebSocket serves a WebSocket endpoint of the application on a GET route; without it the server speaks no
// WebSocket.
//
// CookieSessions is the Sessions implementation: a random ID that only the server issues, in a cookie, with
// the state in a SessionStore, in memory by default, and a tool may supply its own. Issue starts a session
// after a sign-in, End ends it on the server and deletes the cookie.
//
// Authenticator turns a LoginMethod into a session, throttles failed attempts and reports sign-ins,
// sign-outs, failures and throttled attempts to a LoginAudit, for example for auditlog. OnceCode is the
// first method: the application calls Generate and shows the code in its terminal.
//
// Pages serves the sign-in page and the sign-out for these. Sign-out takes POST only, so a tool adds a form
// that posts to LogoutPath.
//
// CSRFToken returns the CSRF token of the session of a request for a form, in the field CSRFField; a script
// may send it in CSRFHeader instead. RequireRecent marks a route that is not public as one for sensitive
// steps, which runs only after a recent sign-in or a confirmation on the page at ReauthPath.
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
// (read with Nonce), nosniff, a same-origin referrer policy and a same-origin resource policy, and grants
// no CORS access. Handlers keep the fixed headers they receive. Static files cannot carry the CSP nonce.
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
// A WebSocket handshake must carry exactly one Origin that is the allowed origin; Origin: null, a missing
// Origin and a Sec-Fetch-Site other than same-origin or none never pass, and the Origin check of the
// WebSocket library stays on besides. The route decides about the session as for any route. A received
// message is bounded in size and the wait for the next one in time, a write has a fixed deadline, the
// connection ends when the server shuts down, and messages are not compressed.
//
// A session ID is 256 random bits that only the server issues, and the store keeps it under a hash of the
// ID. An unknown or expired ID is refused and never creates a session. Issue gives a new ID after a sign-in
// and ends one the request brought along. The cookie is always HttpOnly, SameSite=Strict, Path=/ and
// without Domain. It has the __Host- prefix and Secure, except on a Server that binds to loopback alone,
// which speaks plain HTTP: Safari does not store Secure cookies there and Chrome before version 138
// refuses __Host-. That choice follows the Server and never a request header, and anything but such a
// Server, including a network server behind a proxy that terminates TLS, gets the strict form. Other
// local programs can read cookies of a loopback server whatever their form, which only IDs the server
// issued, the refusal of unknown IDs and the new ID after sign-in answer.
//
// A one-time code is eight symbols without look-alikes, valid once, compared in constant time, replaced only
// by Generate and never by a request; a wrong guess does not use it up. Failed attempts, wrong or throttled,
// count against a limit per peer address and one for all addresses, using ratelimit; a refusal looks the
// same either way. On loopback every client shares one address, so the limit for all addresses is the one
// that holds, and a local program can lock the sign-in for one window by failing on purpose. A LoginAudit
// entry holds the kind, the time and the peer host, never a code or an ID.
//
// The pages are plain, accessible markup with the CSP nonce, one-time-code autocomplete that never blocks
// pasting, and an error that names the field. A sign-in ends in a redirect to a fixed path of the
// application, never one from the request. With Pages, a GET or HEAD request without a session for a route
// that is not Public goes to the sign-in page, and any other method gets 401.
//
// The CSRF token adds to the origin check and never replaces it. CookieSessions refuses an unsafe request of
// a session whose token is missing, wrong or from another or ended session, with 403. The token is 256 bits
// derived from the session ID, so it differs per session and changes with every sign-in, it is compared in
// constant time and read from the header or the form body, never from the URL. Reading the body consumes a
// multipart form, so a handler of one should use a header. A request sent by an HTML form needs the field;
// the sign-out of Pages takes it too when a session exists. The sign-in page has no session to bind to: it
// sets a cookie with the attributes of the session cookie, and its form carries a token derived from that
// cookie's secret, which POST to the sign-in path must return before the code is looked at. A refusal does
// not use up the throttle and shows the page again with a new token. Every refused token goes to LoginAudit
// as csrf-refused, with the peer host only, if SessionOptions.Audit (for sessions) or the Authenticator (for
// the sign-in page) has one. A public route other than those of Pages carries no token check of its own.
//
// A route marked with RequireRecent runs only if the session was signed in or confirmed within
// PagesOptions.ReauthWithin, and the session store keeps that time as SignedIn. An older sign-in sends a GET
// or HEAD request to the confirmation page, which asks for the same code as the sign-in with the same
// throttle and the CSRF token of the session, and returns afterwards to the path the request came from;
// other methods get 403 and the handler does not run. The return target is a local path that starts with
// exactly one slash, without scheme, host, backslash or control character, else the fixed home. Confirming
// keeps the session ID and renews only the time. LoginAudit receives reauth and reauth-failed. Without a
// session a marked route is refused.
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
//   - WebSocketOptions.MaxMessageBytes: the largest received message; a larger one closes the connection.
//     Default: 64 KiB.
//   - WebSocketOptions.IdleTimeout: the wait for the next message before the connection closes. Default: 60
//     seconds.
//   - SessionOptions.Store: keeps the sessions. Default: in memory.
//   - SessionOptions.Idle: ends a session that was not used for this long. Default: 30 minutes.
//   - SessionOptions.Absolute: ends a session this long after it began, however active. Default: 8 hours.
//   - SessionOptions.Audit: receives the requests refused for a missing or wrong CSRF token. Default: no
//     protocol.
//   - OnceCodeOptions.Lifetime: how long a code stays valid. Default: 5 minutes.
//   - OnceCodeOptions.Redactor: learns every code, so a code that reaches a log is removed. Default: none.
//   - AuthOptions.Audit: receives the sign-in events. Default: no protocol.
//   - AuthOptions.Window: how long a failed attempt counts. Default: 10 minutes.
//   - AuthOptions.PerAddress: the failed attempts one peer address may make per Window. Default: 5.
//   - AuthOptions.Global: the failed attempts from all addresses together per Window. Default: 20.
//   - PagesOptions.Prefix: a path put before the paths of the pages, such as "/auth" for "/auth/login".
//     Default: none, so the pages are /login and /logout.
//   - PagesOptions.Home: the fixed path a browser goes to after signing in. Default: "/".
//   - PagesOptions.ReauthWithin: how recent the sign-in or its confirmation must be for a route marked with
//     RequireRecent. Default: 5 minutes.
//   - Options.Log: receives refusals and the errors of the HTTP server. Default: they are discarded.
//   - Options.Redactor: removes known secrets from every log line. Default: the text is logged as it is.
package web
