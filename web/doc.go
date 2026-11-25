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
// time; a long-lived response clears its own deadlines with http.ResponseController.
//
// Every route pattern names its method, so a handler that changes state is not reached by GET. A route
// that is not Public needs a session: Sessions decides, and without it such routes are refused. LoginOff
// serves them without a session and is accepted only on loopback. Handlers keep the fixed headers they
// receive.
//
// Error answers with a page that names only the status, also for unknown paths and methods and for every
// refusal; Redirect sends a browser only to a path on this server. Refusals and server errors go to the
// log through the Redactor, with request values quoted.
package web
