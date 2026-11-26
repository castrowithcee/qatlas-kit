// Command webdemo serves a small browser interface on loopback to try the sign-in of the web building
// block with a real browser, with invented content. It is a workbench, not a program to install; start it
// from a checkout of the module:
//
//	go run ./cmd/webdemo
//
// The program prints the address and a one-time code. Open the address in a browser, enter the code on the
// sign-in page and reach a private page that confirms the session and offers a sign-out button. The code
// works once and for five minutes; press Enter in the terminal for a new one. The terminal also shows the
// protocol entries of sign-ins, sign-outs and failed attempts, which hold no code. Ctrl+C quits.
//
// The optional flag -port sets the loopback port; 0 picks a free one. The program keeps everything in
// memory and uses the public API of the kit only, as a tool built on it would.
package main
