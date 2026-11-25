// Package web serves the browser interface of an application with its protection built in. The
// application supplies routes and handlers; the server applies the same checks to every request, and
// none of them can be switched off.
//
// Listen binds 127.0.0.1 and ::1 by default. Network binds another address and starts only with TLS or
// with declared proxies, for the origins it names and with Sessions; X-Forwarded-* and Forwarded headers
// are removed unless a declared proxy sent them, and without TLS only a proxy is served.
//
// Before a route runs, the server sets a strict Content Security Policy with a fresh nonce per response
// (read with Nonce), nosniff, no referrer and a same-origin resource policy, and grants no CORS access. It
// refuses a Host header other than localhost, 127.0.0.1 or [::1] with any port, or on the network the
// hosts of the allowed origins, which stops DNS rebinding. An unsafe request from another origin is
// refused through Sec-Fetch-Site and Origin; Origin: null never passes, and a request without either
// header comes from a client that is not a browser and passes. Headers and bodies are bounded in size and
// time; a long-lived response other than an event stream clears its own deadlines with
// http.ResponseController.
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
