# Third-party notices

Components derived from `qatlas-cli` and `qatlas-sysmin` retain their original AGPL-3.0 license.

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
| `qatlas-cli/internal/vaultproc/listen_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unix socket listener with single-instance lock and successor handover (`localipc/listen_unix.go`, `localipc/endpoint_unix.go`) |
| `qatlas-cli/internal/vaultproc/runtimedir_linux.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Linux runtime directory (`localipc/runtime_linux.go`) |
| `qatlas-cli/internal/vaultproc/socketdir_darwin.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS socket directory (`localipc/runtime_darwin.go`) |
| `qatlas-cli/internal/vaultproc/peer_linux.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Linux peer verification, user check of a hardened server, replaced program detection, and hardening (`localipc/peer_linux.go`) |
| `qatlas-cli/internal/vaultproc/peer_darwin.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS peer verification, replaced program detection, and hardening (`localipc/peer_darwin.go`) |
| `qatlas-cli/internal/vaultproc/peer_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows peer verification, replaced program detection, and hardening (`localipc/peer_windows.go`) |
| `qatlas-cli/internal/vaultproc/pipe_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows named pipe (`localipc/pipe_windows.go`, `localipc/endpoint_windows.go`) |
| `qatlas-cli/internal/vaultproc/unix_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unix channel tests (`localipc/localipc_unix_test.go`, `localipc/localipc_linux_test.go`) |
| `qatlas-cli/internal/vaultproc/peer_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS channel tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/socketdir_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS socket directory tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/harden_darwin_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS hardening tests (`localipc/peer_darwin_test.go`) |
| `qatlas-cli/internal/vaultproc/windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows channel tests (`localipc/localipc_windows_test.go`) |
| `qatlas-cli/internal/vaultproc/harden_windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows hardening tests (`localipc/localipc_windows_test.go`) |
| `qatlas-cli/internal/vaultproc/client.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Server check before the challenge and peer process id in refusals (`localipc/localipc.go`) |
| `qatlas-cli/internal/vaultproc/handover.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Successor process handle (`background/background.go`) |
| `qatlas-cli/internal/vaultproc/handover_unix_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Handover tests (`localipc/channel_unix_test.go`) |
| `qatlas-sysmin/internal/daemon/listen.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Service socket mode (`localipc/service_linux.go`) |
| `qatlas-sysmin/internal/daemon/peer.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Root or group admission of service clients (`localipc/service_linux.go`) |
| `qatlas-sysmin/internal/client/client.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Service user check of the server (`localipc/service_linux.go`) |
| `qatlas-cli/internal/vaultproc/serve.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Report words and start timeout (`background/background.go`, `background/start.go`) |
| `qatlas-cli/internal/vaultproc/serve_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Detached process start with handover and report pipes on Linux and macOS (`background/start_unix.go`, `background/child.go`) |
| `qatlas-cli/internal/vaultproc/serve_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Detached process start with handover and report pipes on Windows (`background/start_windows.go`) |
| `qatlas-cli/internal/vaultproc/serve_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unsupported platform start (`background/unsupported.go`) |
| `qatlas-cli/internal/vaultproc/serve_unix_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Process start tests (`background/background_test.go`) |
| `qatlas-cli/internal/cli/vaultproc_unix.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Inherited descriptors on Linux and macOS (`background/start_unix.go`) |
| `qatlas-cli/internal/cli/vaultproc_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Inherited standard streams on Windows (`background/start_windows.go`) |
| `qatlas-cli/internal/cli/vaultproc_serve.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Handover reading and report handling (`background/child.go`) |
| `qatlas-cli/internal/cli/vaultproc_linux.go` | Copyright 2026 castrowithcee | AGPL-3.0 | systemd-logind KillUserProcesses detection (`background/logind_linux.go`) |
| `qatlas-cli/internal/cli/vaultproc_linux_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | logind detection tests (`background/logind_linux_test.go`) |
| `qatlas-cli/internal/cli/vaultproc_darwin.go` | Copyright 2026 castrowithcee | AGPL-3.0 | macOS logout behavior (`background/logind_other.go`) |
| `qatlas-cli/internal/cli/vaultproc_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Logout behavior on other systems (`background/logind_other.go`) |
| `qatlas-cli` | Copyright 2026 castrowithcee | AGPL-3.0 | CLI output formats and TOON encoder; error and confirmation handling; help topics and manpage helpers |
| `qatlas-cli/internal/application/compact.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Dropping null members, empty lists, and empty objects (`output/empty.go`) |
| `qatlas-cli/internal/application/original.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Redacting and bounding an external answer (`cli/external.go`) |
| `qatlas-cli/internal/cli/errors.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Next step per route and external answer block (`cli/errors.go`, `cli/external.go`) |
| `qatlas-cli/cmd/gen-man/main.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Help topics section of the manpage (`manpage/manpage.go`) |
| `qatlas-cli/internal/config/config.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration path precedence |
| `qatlas-cli/internal/config/store.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration write preparation and atomic replacement |
| `qatlas-cli/internal/config/store_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Atomic replacement, symlink, and permission tests |
| `qatlas-cli/internal/config/store_windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows lock and backup ACL tests |
| `qatlas-cli/internal/config/fileio.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Retried file reads and store observer |
| `qatlas-cli/internal/config/backup_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Private Unix file and directory modes |
| `qatlas-cli/internal/config/backup_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Private Windows file, directory, and lock ACLs |
| `qatlas-cli/internal/config/transaction.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration revisions, stable lock files, transactions, and revision of a saved value |
| `qatlas-cli/internal/config/transaction_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Transaction and concurrent update tests |
| `qatlas-cli/internal/config/backup.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Configuration backups, first hand version, rotation, and backup copies |
| `qatlas-cli/internal/config/restore.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Backup listing, reading, snapshot, restore, and undo |
| `qatlas-cli/internal/config/backup_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Backup behavior tests |
| `qatlas-cli/internal/config/restore_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Restore behavior tests |
| `qatlas-cli/internal/config/newfile.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Exclusive creation of private files |
| `qatlas-cli/internal/release/release.go` | Copyright 2026 castrowithcee | AGPL-3.0 | SSH signature, checksum and program hash verification (`release/release.go`, `release/program.go`) |
| `qatlas-cli/internal/release/release_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Signature verification tests |
| `qatlas-cli/internal/release/releasetest/releasetest.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Test signing helper (`release/releasetest/releasetest.go`) |
| `qatlas-cli/internal/release/releasetest/server.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Test release server (`release/releasetest/server.go`) |
| `qatlas-cli/internal/selfupdate/semver.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Release version comparison |
| `qatlas-cli/internal/selfupdate/releases.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Release listing and selection |
| `qatlas-cli/internal/selfupdate/selfupdate.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Verified release download and archive extraction |
| `qatlas-cli/internal/selfupdate/replace.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Staged executable replacement and Windows retry |
| `qatlas-cli/internal/tui/text.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Catalog lookup, key placeholders and language preference resolution (`tui/text.go`, `uiprefs/language.go`) |
| `qatlas-cli/internal/tui/keymap.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Key placeholder replacement (`tui/text.go`) |
| `qatlas-cli/internal/tui/text_en.go` | Copyright 2026 castrowithcee | AGPL-3.0 | General English texts (`tui/catalog.go`) |
| `qatlas-cli/internal/tui/text_de.go` | Copyright 2026 castrowithcee | AGPL-3.0 | General German texts (`tui/catalog.go`) |
| `qatlas-cli/internal/tui/text_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Catalog key, format verb and fallback tests (`tui/text_test.go`, `uiprefs/language_test.go`) |
| `qatlas-cli/internal/tui/text_catalog_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Declared ID and placeholder tests (`tui/text_test.go`) |
| `qatlas-cli/internal/uiprefs/uiprefs.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Language preference values (`uiprefs/language.go`) |
| `qatlas-sysmin/internal/tui/text.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Catalog lookup (`tui/text.go`) |
| `qatlas-sysmin/internal/tui/text_en.go` | Copyright 2026 castrowithcee | AGPL-3.0 | General English texts (`tui/catalog.go`) |
| `qatlas-sysmin/internal/tui/text_de.go` | Copyright 2026 castrowithcee | AGPL-3.0 | General German texts (`tui/catalog.go`) |
| `qatlas-sysmin/internal/tui/text_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Catalog key, format verb and fallback tests (`tui/text_test.go`) |
| `qatlas-cli/internal/redact/redact.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Redaction of registered values (`redact/redact.go`, `redact/doc.go`) |
| `qatlas-cli/internal/redact/redact_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Redaction tests (`redact/redact_test.go`) |
| `qatlas-sysmin/internal/redact/redact.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Redaction of registered values (`redact/redact.go`) |
| `qatlas-cli/internal/provider/ratelimit/ratelimit.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Per-key request spacing and registry (`ratelimit/ratelimit.go`, `ratelimit/doc.go`) |
| `qatlas-cli/internal/provider/ratelimit/ratelimit_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Rate limit tests (`ratelimit/ratelimit_test.go`) |
| `qatlas-cli/internal/provider/transport.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Client without redirects and Retry-After (`httpx/transport.go`) |
| `qatlas-cli/internal/provider/cursor.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Request-bound cursors (`httpx/cursor.go`) |
| `qatlas-cli/internal/provider/status.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Status classification and bounded JSON reading (`httpx/status.go`) |
| `qatlas-cli/internal/provider/provider.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Error classes and answer types (`httpx/status.go`, `httpx/original.go`) |
| `qatlas-cli/internal/provider/original.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Bounded error answer reading (`httpx/original.go`) |
| `qatlas-cli/internal/provider/transport_test.go`, `cursor_test.go`, `status_test.go`, `original_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | HTTP mechanics tests (`httpx/*_test.go`) |
| `qatlas-cli/internal/application/schema.go` | Copyright 2026 castrowithcee | AGPL-3.0 | JSON Schema subset validation (`jsonschema/jsonschema.go`) |
| `qatlas-cli/internal/application/application_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Schema validation tests (`jsonschema/jsonschema_test.go`) |
| `qatlas-cli/internal/cli/architecture_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | File walk, import and qualified identifier checks, name matching and shrinking exception list (`archtest/archtest.go`, `archtest/checks.go`) |
| `qatlas-cli/internal/provider/sdk_guard_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Declaration, construction and literal checks (`archtest/checks.go`) |
| `qatlas-cli/internal/tui/keymap_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Key literal check (`archtest/checks.go`) |
| `qatlas-cli/internal/tui/color_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Colour constructor check (`archtest/checks.go`) |
| `qatlas-cli/internal/tui/text_catalog_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Declared text ID check (`archtest/catalog.go`) |
| `qatlas-sysmin/internal/cli/architecture_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Surface import and store handle checks (`archtest/checks.go`) |
| `qatlas-sysmin/internal/tui/keymap_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Key literal check (`archtest/checks.go`) |
| `qatlas-cli/internal/vault/vault.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Vault states and file layout (`vault/vault.go`) |
| `qatlas-cli/internal/vault/crypto.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Key wrapping with scrypt and age encryption (`vault/crypto.go`) |
| `qatlas-cli/internal/vault/format.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Permission error, permission-checked reads and atomic private writes (`vault/file.go`) |
| `qatlas-cli/internal/vault/operations.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Unlock, document reads and writes (`vault/operations.go`) |
| `qatlas-cli/internal/vault/pending.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Pending entries and their merge (`vault/pending.go`) |
| `qatlas-cli/internal/vault/switch.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Encrypt, decrypt, passphrase change and verification (`vault/switch.go`) |
| `qatlas-cli/internal/vault/checkpoint.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Checkpoint and restore (`vault/checkpoint.go`) |
| `qatlas-cli/internal/vault/snapshot.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Snapshot and recipient (`vault/snapshot.go`) |
| `qatlas-cli/internal/vault/status.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Vault status (`vault/vault.go`) |
| `qatlas-cli/internal/vault/settings.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Key derivation and check values from the vault key (`vault/sealed.go`) |
| `qatlas-cli/internal/vault/workfactor.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Work factor limited to test binaries (`vault/vault.go`) |
| `qatlas-cli/internal/vault/terminal.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Passphrase from the controlling terminal (`vault/terminal.go`) |
| `qatlas-cli/internal/vault/terminal_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Non-interactive prompt test (`vault/terminal_test.go`) |
| `qatlas-cli/internal/vault/mode_other.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Private Unix file modes (`vault/perm_other.go`) |
| `qatlas-cli/internal/vault/mode_windows.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows ACL check and private ACLs (`vault/perm_windows.go`) |
| `qatlas-cli/internal/vault/mode_windows_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Windows ACL tests (`vault/perm_windows_test.go`) |
| `qatlas-cli/internal/vault/vault_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Vault behavior tests (`vault/vault_test.go`) |
| `qatlas-sysmin/internal/vault/operations.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Create with the key file written last, kept documents on failed writes (`vault/operations.go`) |
| `qatlas-sysmin/internal/vault/crypto.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Refusal of an empty passphrase on unlock (`vault/crypto.go`) |
| `qatlas-sysmin/internal/vault/snapshot.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Opening with the vault key alone (`vault/snapshot.go`) |
| `qatlas-sysmin/internal/vault/vault.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Concurrency-safe vault value (`vault/vault.go`) |
| `qatlas-sysmin/internal/vault/vault_test.go` | Copyright 2026 castrowithcee | AGPL-3.0 | Vault rejection tests (`vault/vault_test.go`) |
