# mcps

Set up MCP servers for all your AI coding tools from **one list**.
Claude Code, Codex, Cursor, Kiro, OpenCode and AGY each keep MCP config in their own
format and place. mcps keeps your servers in one list; each project picks the ones it needs,
and mcps writes the right config files for every tool.

## Install

    brew tap chavisnguyen/mcps https://github.com/chavisnguyen/mcps
    brew install mcps
    # or: make build && make install

## Quick start

Open a terminal in a project folder and run:

    mcps

The first time, it walks you through two steps:

1. **Your MCP servers**: it finds the MCP servers already set up in your AI tools and lets you
   choose which to add to the mcps list. Nothing is removed from those tools.
2. **This folder**: tick the servers this project should use. mcps then creates the config
   files for each tool installed on your machine (`.mcp.json`, `.codex/config.toml`, ...).

Next time, `mcps` only asks step 2, so you can change a folder's servers whenever you like.

Keys: `↑/↓` move · `space` tick/untick · `←/→` none/all · `enter` confirm · `ctrl+c` cancel.

## Files

| File | What it is |
|---|---|
| `~/.mcps/config/servers.toml` | the mcps list of MCP servers (created for you) |
| `~/.mcps/secrets.env` | API keys moved out of the list (private, mode 0600) |
| `<project>/.mcps.toml` | which servers this project uses |

`servers.toml` can be edited by hand (`mcps init` creates one with commented examples):

    [servers.github]
    cmd = ["npx", "-y", "@modelcontextprotocol/server-github"]
    env = { GITHUB_PERSONAL_ACCESS_TOKEN = "${GITHUB_TOKEN}" }

    [servers.slack-a]            # same server, another account = another entry
    url = "http://localhost:3010/mcp"

    [servers.composio]           # remote server with an auth header
    url = "https://example.com/mcp"
    headers = { x-api-key = "${EXAMPLE_API_KEY}" }

`${NAME}` is filled in from an environment variable, or from `~/.mcps/secrets.env`.
`.mcps.toml`:

    use = ["github", "slack-a"]
    agents = ["claude", "codex"]     # optional: only these tools get files

## Other commands

| Command | What it does |
|---|---|
| `mcps import` | add MCP servers already set up in your AI tools to the mcps list |
| `mcps servers` | show the mcps list |
| `mcps sync` | re-apply this folder's `.mcps.toml` (after editing it by hand) |
| `mcps validate` | check the list and project files, changes nothing |
| `mcps up <server>` | run a server's `install` command from the list |
| `mcps init [git-url]` | create the list, or use a git repo (e.g. private) as the list |
| `mcps add` / `rm` / `list` | ad-hoc edits of your tools' global config |
| `mcps sync -g` | apply `[global] use` from the list to your tools' global config |

Flags: `--dry-run`, `--agents claude,codex`, `--dir PATH`.

## Good to know

- mcps only removes servers **it created** (tracked in `~/.mcps/state.json`). Entries you
  added by hand are never touched. Generated files are disposable: run `mcps sync` to recreate them.
- Every changed file is backed up to `~/.mcps/backups/` first and written atomically; key order
  and unrelated content are preserved.
- Typos in TOML keys are errors. A missing `${NAME}` stops everything before any file is written.
- Private MCP servers: mcps never fetches code. Put the launch command in the list
  (`uvx --from git+ssh://...`, `docker compose up`, ...); your own ssh/docker credentials do the auth.
- mcps does not touch git. Files it creates in a project can contain API keys: add them to
  `.gitignore` (or `.git/info/exclude`) if you don't want them committed.
- Claude Desktop has no per-folder config, so it is skipped for projects.
- Some tools need approval or trust for project-level config (Codex only reads `.codex/config.toml`
  in trusted projects).
- Servers already in a tool's *global* config stay there; the tool will see both.
