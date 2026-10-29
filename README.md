# Outline MCP

A Go stdio MCP server for Outline, authenticated with an API key. One executable provides **setup** and **run**; it does not provide a CLI for calling Outline operations.

## Build

Requires Go 1.26 or newer. On Arch Linux, install Go and Make, then:

```sh
make build       # ./bin/outline-mcp (current platform)
make all         # Linux amd64 and Windows amd64
```

Cross-platform artifacts:

- `./bin/outline-mcp-linux-amd64`
- `./bin/outline-mcp-windows-amd64.exe`
- `./bin/SHA256SUMS`

Both targets disable CGO. On Windows, run `./scripts/build.ps1` in PowerShell to build both targets without Make. The script restores the original build environment variables. Set a release version with `make all VERSION=1.0.0` or `./scripts/build.ps1 -Version 1.0.0`.

Verify locally built binaries with `cd bin && sha256sum --check SHA256SUMS`. On Windows, use `Get-FileHash .\bin\outline-mcp-windows-amd64.exe -Algorithm SHA256` and compare it with the corresponding entry in `SHA256SUMS`.

Builds use `-buildvcs=false` and an explicit version, so source archives and checkouts without accessible Git metadata build the same way. Binaries embed the tool catalog and LLM skill; they do not require the repository at runtime.

## Setup

```sh
./bin/outline-mcp setup
```

On Windows:

```powershell
.\bin\outline-mcp-windows-amd64.exe setup
```

The interactive wizard:

1. Asks for your Outline URL and API key. On a terminal, key input is hidden.
2. Offers optional in-memory history, discarded when the MCP server exits.
3. Detects `codex` and `claude` on PATH and lets you choose a harness. Detection does not silently select an installation when both are available.
4. Shows the configuration and skill destinations before applying changes.
5. Saves the configuration, installs the embedded skill, and registers the user-level `outline` MCP entry through the selected harness CLI.

Setup does not contact Outline or validate the key against your workspace. Existing configuration and different skill content are backed up beside the original file before replacement. Backups containing old credentials should be managed with the same care as the config.

Supported automatic registration:

| Harness | Registration | Skill destination |
|---|---|---|
| Codex | `codex mcp add outline -- … run --config …` | `~/.agents/skills/outline/SKILL.md` |
| Claude Code | `claude mcp add-json --scope user outline …` | `~/.claude/skills/outline/SKILL.md` |
| Generic / undetected | Copyable MCP JSON | User-chosen directory, or skip |

You can choose `--harness codex`, `--harness claude`, or `--harness generic`, and override the skill destination with `--skill-dir DIR`. The destination is the directory that will contain `SKILL.md`.

Generic mode also supports clients whose desktop application is installed but whose CLI is not on PATH. No attempt is made to guess their private configuration layout. Codex uses TOML internally; generic JSON is a common MCP configuration format, not a universal configuration file for every client.

Print a copyable MCP configuration without prompts, credentials, or file writes:

```sh
./bin/outline-mcp setup --print-config
./bin/outline-mcp setup --config /path/to/config.json --print-config
```

The output uses the actual executable and absolute config paths:

```json
{
  "mcpServers": {
    "outline": {
      "type": "stdio",
      "command": "/absolute/path/to/outline-mcp",
      "args": ["run", "--config", "/absolute/path/to/config.json"]
    }
  }
}
```

Merge the `outline` entry into your client's existing config. Some clients use a `servers` wrapper instead of `mcpServers`. Keep the binary at its registered path, and reload/restart the harness after installation. Setup reports partial success and prints manual JSON if harness registration fails; it does not retry unknown registration writes.

The repo skill lives at [`skills/outline/SKILL.md`](skills/outline/SKILL.md). It explains search/edit workflows, pagination, reaction routing, unknown write outcomes, and recovering context through the history resource. Setup embeds and installs this same file.

## Run and configuration

```sh
./bin/outline-mcp run
./bin/outline-mcp run --config /path/to/config.json
```

`run` reserves stdout for MCP messages. Startup errors and diagnostics go to stderr. Stdin closing or interrupting the process shuts down the server. No HTTP MCP listener is started.

The default config is `outline-mcp/config.json` inside Go's OS user-config directory: `$XDG_CONFIG_HOME` (or `~/.config`) on Linux, `%AppData%` on Windows. `--config` overrides `OUTLINE_CONFIG`, which overrides that default.

Example configuration (do not commit real keys):

```json
{
  "base_url": "https://your-outline.example",
  "api_key": "YOUR_OUTLINE_API_KEY",
  "history": false,
  "history_limit": 500,
  "timeout_seconds": 60
}
```

| Environment variable | Overrides |
|---|---|
| `OUTLINE_BASE_URL` | Outline base URL; `/api` is appended if absent |
| `OUTLINE_API_KEY` | API key, sent as `Authorization: Bearer …` |
| `OUTLINE_HISTORY` | `true` or `false` |
| `OUTLINE_HISTORY_LIMIT` | Receipts retained in RAM per server run, 1–10,000; default 500 |
| `OUTLINE_TIMEOUT_SECONDS` | Request timeout, 1–600 seconds; default 60 |

Environment values take precedence over saved fields. A missing config file is allowed when environment variables provide authentication. HTTPS is required except for loopback HTTP development servers. Redirects are not followed, preventing credentials from being forwarded to a redirected host. Set the canonical instance URL.

Keys are stored locally as plaintext, with mode 0600 on Unix. On Windows the user-config directory's ACLs govern access; setup does not install custom Windows ACLs. MCP registration contains only a path to this configuration, not the key. The Go MCP SDK has a transitive OAuth dependency, but this server implements no OAuth authentication flow.

## Tool surface

**74 tools**, one per included endpoint in the pinned [Outline OpenAPI specification](https://github.com/outline/openapi/tree/e3d5671461fc8c292286b432af0ce1f87dba5345).

| Category | Count |
|---|---:|
| Collections | 16 |
| Comments | 9 |
| DataAttributes | 5 |
| Documents | 27 |
| Pins | 5 |
| Reactions | 1 |
| Revisions | 4 |
| Templates | 7 |

Names use snake case: `documents.info` → `outline_documents_info`, `documents.answerQuestion` → `outline_documents_answer_question`, `dataAttributes.create` → `outline_data_attributes_create`.

Excluded: `collections.import`, `collections.export`, `collections.export_all`, `documents.import`, `documents.export`, `revisions.export`, `documents.empty_trash`. Other API categories are not exposed. Ordinary delete, archive, and restore operations remain available and are subject to the API key's permissions.

Each tool has its own JSON Schema, including required fields, enums, nested objects, and pagination. OpenAPI references and nullable fields are converted for MCP. Unknown top-level arguments are rejected. The API remains authoritative for semantic validation and permissions; published schemas sometimes leave fields optional that a particular operation needs in practice. The server does not invent extra fields or silently apply defaults.

Successful results contain `response` (the original API JSON object, including pagination/policies) and `receipt` (outcome and optional history ID). Tool failures have `isError: true`; both text and structured content include `error.endpoint`, a stable `code`, an explanatory `message`, and actionable `next_step`. Errors include HTTP `status`, bounded/redacted API `detail`, and `retry_after` when available. Local validation failures say which schema constraint failed and return `receipt.outcome=not_sent`.

Authentication, permissions, missing objects, validation, conflicts, rate limits, DNS/TLS, cancellation, timeouts, redirects, server failures, and oversized/invalid responses have distinct explanations. `outcome_unknown=true` means a mutation may already have succeeded and must be checked before replaying it. Read-only failures receive read-specific retry advice. No operation is automatically retried. Responses larger than 16 MiB fail explicitly, so request bounded pages. The bundled skill contains a recovery table and instructions for reporting these errors clearly.

## History after compaction

Enable history through setup or `OUTLINE_HISTORY=true`. It is held **only in RAM**, with no history files, database, or persistence across restarts. API-key/setup configuration remains on disk; action history does not.

The server exposes one resource with a fresh identifier each run: `outline://history/<run-id>`. Discover its exact URI with `resources/list`, or use `receipt.history_uri` from a tool result. Old resource URIs expire when the process exits. This adds no tools.

Receipts contain timestamps, endpoint/tool, read-only status, selected target IDs, returned IDs (capped at 100), and failure status/error code. They omit API keys, request text, titles, document/comment bodies, full responses, and server error messages. The oldest receipt is evicted when the configured limit is reached, including when requests overlap. A late completion does not resurrect an evicted entry.

History is process-wide, not implicitly conversation-specific. A pooled server may serve unrelated conversations. The server cannot discover the host's task identity or detect compaction. Compaction preserves RAM history only while the same server process remains alive. Restart, exit, or crash discards everything; an empty recap does not mean no earlier operation happened. Unknown/in-progress writes must be checked against current Outline state before being repeated.

The [2026-07-28 MCP lifecycle](https://modelcontextprotocol.io/specification/2026-07-28/basic) is stateless at the protocol level: requests carry their own version/capabilities, and `server/discover` replaces reliance on an initialization handshake. A stdio process may be reused across tasks. Stateless requests do not require spawning a new subprocess for each call. The run-specific history resource is explicitly identified and never used as a conversation/session identity. This server supports both `2026-07-28` and legacy `2025-11-25` clients through the Go SDK.

Legacy `history_id`/`history_dir` fields and their environment variables are no longer used. Existing history files are neither loaded nor modified. If the MCP host cannot read resources, it cannot access the recap; no extra history tool is registered.

## Development and verification

```sh
make test
make check
go test -race ./...
make generate
```

Tests use temporary directories, injected harness commands, and a loopback mock Outline API. The subprocess integration test exercises both modern discovery and legacy initialization over real stdio pipes, discovers and dispatches all 74 tools, rejects invalid arguments before HTTP dispatch, and verifies that history exists during a run but vanishes on restart without history files being written. Tests do not modify live harness settings or contact your Outline instance.

The source specification and upstream license are in `api/`; `api/SOURCE` records the commit. `make generate` regenerates `internal/catalog/tools.json` without network access and stops if the selected operation count changes. Review schema changes before replacing the pinned spec.

Cross-compilation establishes that the Windows target builds; running setup and MCP on an actual Windows host remains a separate acceptance check. The same applies to compatibility with your particular Outline deployment and API-key permissions.

Setup references: [Codex MCP](https://developers.openai.com/codex/mcp), [Codex skills](https://developers.openai.com/codex/skills), [Claude Code MCP](https://code.claude.com/docs/en/mcp), [Claude Code skills](https://code.claude.com/docs/en/skills).

## GitHub CI and draft releases

The workflows work in any GitHub repository without hard-coded owner/repository names or a personal access token. The built-in `GITHUB_TOKEN` is read-only for CI; only the draft-release job receives `contents: write`.

- **CI:** every branch push and pull request runs dependency verification, vet, native tests/builds on Linux and Windows x64, and Linux race checks. Linux also checks formatting and verifies the generated catalog is current. The two binaries are retained as workflow artifacts for 14 days.
- **Draft release:** pushing a tag such as `1.0.0` runs the same CI first, then attaches both binaries, `SHA256SUMS`, `build-info.json` (tag and source commit SHA), `LICENSE`, and generated `THIRD_PARTY_NOTICES.txt` to a draft release. The tag version is embedded in each binary.
- **Verification:** the workflow downloads all uploaded assets and checks them against the local SHA-256 manifest. Drafts remain unpublished for manual review. Prerelease tags such as `v0.2.0-rc.1` are marked as prereleases.
- **Reruns:** an existing draft can have its generated assets replaced; an already published release is never updated by the workflow. API/network failures abort instead of being interpreted as a missing release. If an upload partially fails, inspect the draft before rerunning.

After uploading the source to your own GitHub repository, create a version tag from the intended release commit:

```sh
git tag -a 1.0.0 -m "Outline MCP 1.0.0"
git push origin 1.0.0
```

Review the **Draft release** Actions run and the draft's assets, then publish manually in GitHub Releases. You can also dispatch the workflow manually **on an existing version tag**; dispatching it on a branch is rejected. Repository/organization settings must allow GitHub Actions and the job's requested token permissions.

The workflow actions are pinned to commit SHAs. Dependabot proposes monthly updates for Actions and Go modules. Runtime credentials, local harness folders, generated binaries, work files, and config backups are ignored by Git.

On Linux, release files downloaded through a browser may need `chmod +x outline-mcp-linux-amd64` before running `./outline-mcp-linux-amd64 setup`. The skill is already embedded in each binary. Keep the license and third-party notices with redistributed binaries.

## License

Project code is [MIT licensed](LICENSE). The vendored Outline API specification retains its [BSD-3-Clause license](api/LICENSE). Release assets include the license texts for the Go runtime and linked modules in `THIRD_PARTY_NOTICES.txt`.
