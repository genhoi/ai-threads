"""Grok CLI: `$GROK_HOME/sessions/**/summary.json`, переписка в `chat_history.jsonl` рядом,
запущенные сессии в `$GROK_HOME/active_sessions.json`."""

from ..sources.common import UUID, clean_title, read_json, timestamp
from .. import live as live_registry
from .base import JsonlAgent, shell_prompt
from .wire import wire_events


class Grok(JsonlAgent):
    id = "grok"
    name = "Grok"
    color = "#5a5e66"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="Grok"><rect x="0.5" y="0.5" width="23" height="23" rx="5.5" '
            'fill="#101114" stroke="#5a5e66"></rect><circle cx="12" cy="12" r="5.8" stroke="#ffffff" stroke-width="2" '
            'fill="none"></circle><path d="M6.6 17.4L17.4 6.6" stroke="#ffffff" stroke-width="2" stroke-linecap="round">'
            '</path></svg>')
    home_env = "GROK_HOME"
    home_default = ".grok"
    programs = ("grok",)
    pattern = "sessions/**/summary.json"
    resume = "grok --resume {id}"
    skip_flag = "--always-approve"
    runner = True

    def journal_path(self, path):
        return path.parent / "chat_history.jsonl"

    def message_of(self, row):
        return row.get("type"), row.get("content"), row.get("timestamp") or row.get("time")

    def read_info(self, path, journal):
        data = read_json(path)
        if not data or data.get("session_kind") == "subagent":
            return None
        return data

    def describe(self, path, data, head, rows, values, extra):
        info = data.get("info") or {}
        values["id"] = info.get("id") or path.parent.name
        values["cwd"] = info.get("cwd") or ""
        values["title"] = clean_title(data.get("generated_title") or data.get("session_summary")) or values["title"]
        values["branch"] = data.get("head_branch") or ""
        values["updated"] = timestamp(data.get("last_active_at") or data.get("updated_at"), values["updated"])
        values["created"] = timestamp(data.get("created_at"), values["created"])
        values["auto"] = data.get("session_kind") == "headless"
        values["by"] = "grok headless" if values["auto"] else "я"

    def valid_id(self, sid):
        return bool(UUID.fullmatch(sid))

    def live(self, sessions):
        """Реестр `active_sessions.json`. Формат не описан; в grok 1.0.40 у записи четыре поля:
        session_id, pid, cwd, opened_at. Поля «ждёт / работает» нет, поэтому живая запись — это
        work. Короткий `grok -p` реестр не заполняет."""
        records = live_registry.read_small_json(self.home() / "active_sessions.json")
        if not isinstance(records, list):
            return []
        found = []
        for record in records:
            if not isinstance(record, dict):
                continue
            session_id = _field(record, "session_id", "sessionId")
            if not isinstance(session_id, str) or not session_id:
                continue
            key = f"grok:{session_id}"
            if key not in sessions or not live_registry.process_alive(_field(record, "pid"), None):
                continue
            since = live_registry.unix_seconds(_field(record, "opened_at", "openedAt"))
            since = 0 if since is None else since
            found.append((key, {"state": "work", "where": "Grok", "since": since}, since))
        return found

    def headless(self, program, prompt, *, instructions, add_dirs, out_file):
        argv = [program, "-p", shell_prompt(instructions, prompt), "--sandbox", "read-only",
                "--output-format", "streaming-messages-json"]
        return argv, None

    def stream_events(self, row):
        return wire_events(row)


def _field(record: dict, *names: str):
    for name in names:
        if name in record:
            return record[name]
    return None


AGENT = Grok()
