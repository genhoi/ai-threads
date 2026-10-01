"""Claude Code: `$CLAUDE_HOME/projects/*/<uuid>.jsonl`, история ввода в `history.jsonl`,
запущенные сессии в `$CLAUDE_HOME/sessions/*.json`."""

from ..sources.common import UUID, claude_user_text, clean_title, update_index
from .. import live as live_registry
from .base import JsonlAgent, first_title, shell_prompt
from .wire import wire_events

_STATE = {"idle": "wait", "waiting": "wait", "busy": "work", "shell": "work"}


class Claude(JsonlAgent):
    id = "claude"
    name = "Claude Code"
    color = "#c4613a"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="Claude Code"><rect x="0" y="0" width="24" height="24" rx="6" '
            'fill="#c4613a"></rect><path d="M12 5.2v13.6M6.1 8.6l11.8 6.8M6.1 15.4l11.8-6.8" stroke="#ffffff" '
            'stroke-width="2.2" stroke-linecap="round" fill="none"></path></svg>')
    home_env = "CLAUDE_HOME"
    home_default = ".claude"
    programs = ("claude",)
    pattern = "projects/*/*.jsonl"
    resume = "claude --resume {id}"
    skip_flag = "--dangerously-skip-permissions"
    runner = True

    def message_of(self, row):
        if row.get("type") not in ("user", "assistant") or row.get("isSidechain"):
            return None
        role = row["type"]
        value = (row.get("message") or {}).get("content")
        if role == "user":
            value = claude_user_text(row, value)
        return role, value, row.get("timestamp")

    def read_info(self, path, journal):
        return {} if UUID.fullmatch(path.stem) else None

    def describe(self, path, info, head, rows, values, extra):
        user = next((r for r in head if r.get("type") == "user"), {})
        values["auto"] = user.get("entrypoint") == "sdk-cli"
        values["by"] = "claude -p" if values["auto"] else "я"
        for row in rows:
            if row.get("type") in ("user", "assistant") and not row.get("isSidechain"):
                values["cwd"] = row.get("cwd") or values["cwd"]
                values["branch"] = row.get("gitBranch") or values["branch"]
            if row.get("type") == "ai-title" and clean_title(row.get("aiTitle")):
                extra["ai_title"] = clean_title(row["aiTitle"])
        extra["journal_cwd"] = values["cwd"]

    def valid_id(self, sid):
        return bool(UUID.fullmatch(sid))

    def valid_extra(self, entry):
        return isinstance(entry.get("journal_cwd"), str) and isinstance(entry.get("ai_title", ""), str)

    def indexes(self, cache):
        def apply(values, row):
            if isinstance(row.get("sessionId"), str):
                sid = row["sessionId"]
                previous = values.get(sid, {})
                values[sid] = {"title": previous.get("title", clean_title(row.get("display"))),
                               "cwd": row.get("project") or previous.get("cwd", "")}

        def check(value):
            return isinstance(value, dict) and all(isinstance(value.get(k), str) for k in ("title", "cwd"))
        return update_index(self.home() / "history.jsonl", cache, "claude-index", apply, check)

    def finish(self, session, entry, names):
        indexed = names.get(session.id, {})
        session.title = entry.get("ai_title") or indexed.get("title") or first_title(entry)
        session.cwd = entry["journal_cwd"] or indexed.get("cwd", "")

    def live(self, sessions):
        directory = self.home() / "sessions"
        if not directory.is_dir():
            return []
        found = []
        for path in sorted(directory.glob("*.json")):
            record = live_registry.read_small_json(path)
            if not isinstance(record, dict):
                continue
            session_id = record.get("sessionId")
            if not isinstance(session_id, str) or not session_id:
                continue
            key = f"claude:{session_id}"
            state = _STATE.get(record.get("status"))
            if key not in sessions or state is None:
                continue
            if not live_registry.process_alive(record.get("pid"), record.get("procStart")):
                continue
            since = live_registry.unix_seconds(record.get("statusUpdatedAt"))
            if since is None:
                since = live_registry.unix_seconds(record.get("updatedAt"))
            where = _where(record.get("kind"), record.get("name"))
            if since is None or not where:
                continue
            found.append((key, {"state": state, "where": where, "since": since}, since))
        return found

    def headless(self, program, prompt, *, instructions, add_dirs, out_file):
        argv = [program, "-p", "--output-format", "stream-json", "--verbose",
                "--tools", "Read", "--permission-mode", "dontAsk"]
        for directory in add_dirs:
            argv += ["--add-dir", str(directory)]
        return argv, shell_prompt(instructions, prompt)

    def stream_events(self, row):
        return wire_events(row)


def _where(kind, name) -> str:
    if kind == "interactive":
        where = "Claude Code в терминале"
    elif kind == "bg":
        where = "фоновая сессия Claude Code"
    else:
        return ""
    if isinstance(name, str) and name.strip():
        where = f"{where} · {name.strip()}"
    return where


AGENT = Claude()
