<p align="center"><img src="docs/images/banner.png" alt="Nit" width="100%"></p>

# Nit

**English** · [Русский](README.ru.md)

Nit (Russian for "thread") is a local web app for working with coding agents: Claude Code, Codex,
Grok, Kimi and Cursor Agent. You start agents in it, answer them in a real terminal, find old sessions
and get a summary of the last few days. From your phone too.

<p align="center"><img src="docs/images/screen-session.png" alt="Claude Code asks for permission to run a command; on the left, who waits for you, who is working, and the projects" width="100%"></p>

The interface is in Russian for now.

## What it does

- **Start.** A new session in the project folder or in a separate copy of the repository (git worktree)
  with its own branch. If another agent already works in the folder, Nit suggests a copy so the agents
  don't edit the same files. The model and reasoning effort are picked in the same place.
- **Who waits for you.** The left panel splits sessions into "waiting for you" and "working". An agent
  that crashed shows up among the waiting ones with its exit code. Alt+↓ jumps to the next waiting
  session, even from the terminal.
- **A real terminal.** Each agent runs in its own CLI inside tmux. Close the page or restart Nit and
  the agent keeps working. After a reboot Nit shows which sessions were open and resumes the ones
  you pick.
- **Sessions started outside Nit.** Nit reads the agents' own logs, so it sees them too and can resume
  them here.
- **Search.** Ctrl+K searches titles, branches, tasks and paths, and optionally the conversation. If
  that finds nothing, you can look for an exact match in all logs. History groups sessions by day and
  filters them by project and agent.
- **Summaries.** For one session, and for the last 3, 5 or 7 days across all projects. One of the
  installed agents writes the summary: Nit runs it without the UI and hands it the logs.
- **Finishing a task.** "Finish" stops the agent, closes the terminal and deletes the copy with its
  branch. The copy stays if it has uncommitted files, the branch was never pushed, or another agent
  works in it. Nit says which one it was.
- **Phone.** Who is waiting, the terminal screen as text, replies and quick keys, new sessions.

<p align="center">
<img src="docs/images/screen-home.png" alt="Home: starting a new session" width="32%">
<img src="docs/images/screen-search.png" alt="Session search" width="32%">
<img src="docs/images/screen-summary.png" alt="Summary of the last three days" width="32%">
</p>

ZCode sessions show up in lists and search, but only the ZCode app can resume them.

Interface design and use cases are in [docs/concept.md](docs/concept.md), the architecture in
[docs/plan.md](docs/plan.md), phone access in [docs/phone-access.md](docs/phone-access.md) (all in
Russian). Version 1, in Python, lives on the `v1` branch.

## Requirements

- Linux or WSL 2 (not tested on macOS);
- tmux 3.2 or newer;
- at least one agent: Claude Code, Codex, Grok, Kimi or Cursor Agent;
- Go 1.27.1 or newer to build. Node.js is needed only to change the UI: the built UI is in `web/dist`.

## Install

```bash
git clone https://github.com/genhoi/ai-threads.git && cd ai-threads
go build -ldflags "-s -w -X main.version=0.1.0" -o ~/.local/bin/nit ./cmd/nit
nit                 # http://127.0.0.1:8765, change the port with -port
```

To keep Nit running after you close the terminal, install it as a systemd user service. Pass your own
PATH: systemd's is shorter and doesn't include the agents.

```bash
cat > ~/.config/systemd/user/nit.service <<EOF
[Unit]
Description=Nit
After=network-online.target

[Service]
ExecStart=%h/.local/bin/nit
Environment="PATH=$PATH"
Environment=SHELL=/bin/bash
Environment=LANG=C.UTF-8
Restart=on-failure

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload && systemctl --user enable --now nit
journalctl --user -u nit -f        # logs
```

## Data

- settings: `~/.config/ai-threads/settings.json` (or `$AI_THREADS_CONFIG`);
- names, pins, summaries, the log cache and phone keys: `~/.local/share/ai-threads/`
  (or `$AI_THREADS_DATA`);
- repository copies for parallel agents: `~/.nit/worktrees/<project>/<branch>`;
- terminals: tmux with its own socket `ai-threads`: `tmux -L ai-threads ls`;
- the Tailscale node for phone access: `~/.threadgate`.

What each file holds, every settings field and the environment variables are in
[docs/plan.md](docs/plan.md) (in Russian).

## Phone

<img src="docs/images/screen-phone.png" alt="Nit on a phone: who waits, who works, projects" width="260" align="right">

Settings → «Доступ с телефона» → turn it on. Nit starts a Tailscale node inside its own process and
shows a QR code with a link. You need a free Tailscale account; no static IP is required. The address
opens only from devices in your Tailscale network, so the phone needs the Tailscale app. The
«Из интернета» switch (Tailscale Funnel) opens the address to the whole internet, and then the app
isn't needed. Either way you sign in with the key from the QR code.

The phone shows who waits for you and who is working. A session is shown as the terminal screen
with wrapped lines. Reply with text or quick keys: 1, 2, 3, Enter, Esc, Ctrl+C, arrows. The terminal
size on the computer stays the same.

Details are in [docs/phone-access.md](docs/phone-access.md) (in Russian).

<br clear="right">

## Development

```bash
go test ./...                      # server
cd web && npm ci && npm run build  # UI; npm run dev rebuilds on change
```

Open the UI with design sample data and no backend: `http://127.0.0.1:8765/?mock`. Modes are described in
[web/README.md](web/README.md).
