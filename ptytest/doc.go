// Package ptytest runs a program at a pseudo-terminal of its own, so tests can drive its terminal user
// interface and prompts the way a person at a terminal would. It is meant for tests only.
//
// Start opens the terminal and runs the Command there: on Linux and macOS a pseudo-terminal that is the
// program's controlling terminal and standard streams, on Windows a pseudo console (ConPTY, Windows 10 1809
// or later). The Terminal collects everything the program writes, escape sequences included. Write types
// input; a terminal sends "\r" for the Enter key. WaitFor waits for a state of the output instead of a
// fixed sleep, and Echo tells whether the terminal still echoes input, so a test types a secret only once
// the program switched echo off. Wait returns the exit code, Kill and Close end the program. ProcessAlive
// and KillProcess check and end a process by its id, such as one the program started in the background.
//
// # Always enforced
//
// The program gets exactly the environment the caller passes, so a test isolates its configuration through
// it; nothing is inherited, except that Windows adds SYSTEMROOT when it is missing, since the system does
// not work without it. The program path is not looked up in PATH.
//
// Start refuses a command without a program path, with a NUL byte in a value, with an environment entry
// without '=' or with a terminal size outside 1 to 32767 in either direction, the range all platforms
// accept (ErrInvalidCommand). ProcessAlive and KillProcess refuse ids that name no single process
// (ErrInvalidProcess), since on Unix 0 and negative ids reach whole process groups. A platform without
// pseudo-terminals, and Echo on Windows, whose pseudo console keeps the console mode to itself, return an
// error that wraps errors.ErrUnsupported.
//
// Kill and Close end the program, not processes it started. After the program ended, its output is awaited
// only for five seconds, since such a process may still hold the terminal. Wait kills the program when its
// timeout passes first. The pseudo console on Windows renders the screen itself, so its output can differ
// from what the program wrote in order and escape sequences. Automated runs do not replace checks in real
// terminal emulators.
//
// # Options
//
//   - Command.Env: the complete environment of the program. Default: nil, an empty environment.
//   - Command.Dir: the working directory of the program. Default: empty, the current directory.
//   - Command.Size: the terminal size in character cells. Default: zero, 80 columns and 24 rows.
package ptytest
