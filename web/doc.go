// Package web serves the browser interface of an application with its protection built in. The
// application supplies routes and handlers; the server applies the same checks to every request, and
// none of them can be switched off.
//
// Listen binds 127.0.0.1 and ::1 by default. Network binds another address and starts only with TLS or
// with declared proxies, for the origins it names and with Sessions; X-Forwarded-* and Forwarded headers
// are removed unless a declared proxy sent them, and without TLS only a proxy is served.
//
// Before a route runs, the server sets a strict Content Security Policy with a fresh nonce per response (read
// with Nonce), nosniff, a same-origin referrer policy and a same-origin resource policy, and grants no CORS
// access. It refuses a Host header other than localhost, 127.0.0.1 or [::1] with any port, or on the network
// the hosts of the allowed origins, which stops DNS rebinding. An unsafe request from another origin is
// refused through Sec-Fetch-Site and Origin; Origin: null never passes, and a request without either header
// comes from a client that is not a browser and passes. Headers and bodies are bounded in size and time; a
// long-lived response other than an event stream clears its own deadlines with http.ResponseController.
//
// Events serves a text/event-stream from a Source of the application and resumes it after the
// Last-Event-ID the browser sends back, which is bounded in length and passed on unchanged. The handshake
// must not come from another site and its Origin, if present, must be the allowed origin; Origin: null
// never passes. The route decides about the session as for any route. A stream gets a heartbeat, a
// deadline per write, a bounded event size and lifetime, and a bounded number of open streams per handler;
// it ends when the server shuts down. An event ID or type with a line break or NUL ends the stream.
//
// Every route pattern names its method, so a handler that changes state is not reached by GET. A route
// that is not Public needs a session: Sessions decides, and without it such routes are refused. LoginOff
// serves them without a session and is accepted only on loopback. Handlers keep the fixed headers they
// receive.
//
// CookieSessions is the Sessions implementation. A session is a random 256 bit ID that only the server
// issues, in a cookie that is always HttpOnly, SameSite=Strict, Path=/ and without Domain. The server keeps
// the state in a SessionStore under a hash of the ID, in memory by default, and a tool may supply its own.
// A session ends after 30 minutes without use or 8 hours after it began, unless SessionOptions say less
// or more; an unknown or expired ID is refused and never creates a session. Issue starts a session with a
// new ID after a sign-in and ends one the request brought along, End ends it on the server and deletes the
// cookie. The cookie has the __Host- prefix and Secure, except on a Server that binds to loopback alone,
// which speaks plain HTTP: Safari does not store Secure cookies there and Chrome before version 138
// refuses __Host-. That choice follows the Server and never a request header, and anything but such a
// Server, including a network server behind a proxy that terminates TLS, gets the strict form. Other
// local programs can read cookies of a loopback server whatever their form, which only IDs the server
// issued, the refusal of unknown IDs and the new ID after sign-in answer.
//
// Authenticator turns a LoginMethod into a session. OnceCode is the first method: the application calls
// Generate and shows the code in its terminal; it is eight symbols without look-alikes, valid once and for
// five minutes, compared in constant time, replaced only by Generate and never by a request. A wrong guess
// does not use it up. Failed attempts, wrong or throttled, count against a limit per peer address and
// one for all addresses, per ten minutes, using ratelimit; a refusal looks the same either way. On
// loopback every client shares one address, so the limit for all addresses is the one that holds, and
// a local program can lock the sign-in for one window by failing on purpose. LoginAudit receives
// sign-ins, sign-outs, failures and throttled attempts, for example for auditlog; an entry holds the
// kind, the time and the peer host, never a code or an ID.
//
// Pages serves the sign-in page and the sign-out for these: plain, accessible markup with the CSP nonce,
// one-time-code autocomplete that never blocks pasting, and an error that names the field. A sign-in ends in a
// redirect to a fixed path of the application, never one from the request. Sign-out takes POST only, so a
// tool adds a form that posts to LogoutPath. With Pages, a GET or HEAD request without a session for a
// route that is not Public goes to the sign-in page, and any other method gets 401.
//
// Static serves the files of an fs.FS such as an embed.FS for GET and HEAD, mounted as a route like
// "GET /". It reads the files once and answers only for a path that names one of them: no directory
// listing, no redirect, no path outside the file system and no file or directory whose name starts with a
// dot. The Content-Type is fixed by extension, application/octet-stream for an unknown one, next to the
// fixed nosniff header. Each file has an ETag of its content with Cache-Control: no-cache, so a browser
// revalidates and receives 304 until the content changes. Static files cannot carry the CSP nonce.
//
// Error answers with a page that names only the status, also for unknown paths and methods and for every
// refusal; Redirect sends a browser only to a path on this server. Refusals and server errors go to the
// log through the Redactor, with request values quoted.
package web
