# Third-party notices

Components derived from `qatlas-cli` retain their original AGPL-3.0 license.

| Source | Copyright | License | Included components |
| --- | --- | --- | --- |
| `qatlas-cli/internal/i18n/detect_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Locale detection on non-Windows systems |
| `qatlas-cli/internal/i18n/detect_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows preferred UI language detection |
| `qatlas-cli/internal/i18n/i18n.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Language parsing and catalog lookup |
| `qatlas-cli/internal/i18n/i18n_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Language package tests |
| `qatlas-cli/internal/projectpath/projectpath.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Project path resolution |
| `qatlas-cli/internal/projectpath/projectpath_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Project path tests |
| `qatlas-cli/internal/filelock/doc.go` | Copyright 2026 castrowithcee | AGPL-3.0 | File lock package documentation |
| `qatlas-cli/internal/filelock/lock_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Advisory file locking on non-Windows systems |
| `qatlas-cli/internal/filelock/lock_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | File locking on Windows |
| `qatlas-cli/internal/fileretry/fileretry.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Bounded retry for transient file errors |
| `qatlas-cli/internal/fileretry/fileretry_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | File retry tests |
| `qatlas-cli/internal/fileretry/transient_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Transient file error classification on non-Windows systems |
| `qatlas-cli/internal/fileretry/transient_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Transient Windows file error classification |
| `qatlas-cli/internal/vaultproc/vaultproc.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Local channel endpoint, socket path and peer checks (`localipc/localipc.go`, `localipc/endpoint_unix.go`) |
| `qatlas-cli/internal/vaultproc/dial_socket.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unix socket dialing (`localipc/endpoint_unix.go`) |
| `qatlas-cli/internal/vaultproc/listen_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unix socket listener with single-instance lock (`localipc/listen_unix.go`, `localipc/endpoint_unix.go`) |
| `qatlas-cli/internal/vaultproc/runtimedir_linux.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Linux runtime directory (`localipc/runtime_linux.go`) |
| `qatlas-cli/internal/vaultproc/socketdir_darwin.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS socket directory (`localipc/runtime_darwin.go`) |
| `qatlas-cli/internal/vaultproc/peer_linux.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Linux peer verification and hardening (`localipc/peer_linux.go`) |
| `qatlas-cli/internal/vaultproc/peer_darwin.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS peer verification and hardening (`localipc/peer_darwin.go`) |
| `qatlas-cli/internal/vaultproc/peer_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows peer verification and hardening (`localipc/peer_windows.go`) |
| `qatlas-cli/internal/vaultproc/pipe_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows named pipe (`localipc/pipe_windows.go`, `localipc/endpoint_windows.go`) |
| `qatlas-cli/internal/vaultproc/unix_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unix channel tests (`localipc/localipc_unix_test.go`, `localipc/localipc_linux_test.go`) |
| `qatlas-cli/internal/vaultproc/peer_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS channel tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/socketdir_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS socket directory tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/harden_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS hardening tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows channel tests (`localipc/localipc_windows_test.go`) |
| `qatlas-cli/internal/vaultproc/harden_windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows hardening tests (`localipc/localipc_windows_test.go`) |
| `qatlas-cli` | Copyright 2026 castrowithcee | AGPL-3.0 | CLI output formats and TOON encoder; error and confirmation handling; help topics and manpage helpers |
| `qatlas-cli/internal/config/config.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration path precedence |
| `qatlas-cli/internal/config/store.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration write preparation and atomic replacement |
| `qatlas-cli/internal/config/store_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Atomic replacement, symlink, and permission tests |
| `qatlas-cli/internal/config/store_windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows lock and backup ACL tests |
| `qatlas-cli/internal/config/fileio.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Retried file reads |
| `qatlas-cli/internal/config/backup_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Private Unix file and directory modes |
| `qatlas-cli/internal/config/backup_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Private Windows file, directory, and lock ACLs |
| `qatlas-cli/internal/config/transaction.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration revisions, stable lock files, and transactions |
| `qatlas-cli/internal/config/transaction_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Transaction and concurrent update tests |
| `qatlas-cli/internal/config/backup.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration backups, first hand version, and rotation |
| `qatlas-cli/internal/config/restore.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Backup listing, reading, and restore |
| `qatlas-cli/internal/config/backup_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Backup behavior tests |
| `qatlas-cli/internal/config/restore_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Restore behavior tests |
