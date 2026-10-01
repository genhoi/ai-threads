"""Kimi Code CLI: `$KIMI_CODE_HOME/sessions/*/session_*/state.json`, переписка в
`agents/main/wire.jsonl`, история ручного ввода в `user-history/*.jsonl`."""

import json
import re

from ..sources.common import UUID, clean_title, prefix, read_json, timestamp, update_index
from .base import JsonlAgent, toml_top_level


class Kimi(JsonlAgent):
    id = "kimi"
    name = "Kimi"
    color = "#2a6fd6"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="Kimi"><rect x="0" y="0" width="24" height="24" rx="6" '
            'fill="#2a6fd6"></rect><path d="M14.8 5.4a6.9 6.9 0 1 0 4 11.5 5.4 5.4 0 0 1-4-11.5z" fill="#ffffff"></path></svg>')
    home_env = "KIMI_CODE_HOME"
    home_default = ".kimi-code"
    programs = ("kimi",)
    program_fallbacks = ("bin/kimi",)
    pattern = "sessions/*/session_*/state.json"
    resume = "kimi --session {id}"
    skip_flag = "--auto"
    runner = True

    def journal_path(self, path):
        return path.parent / "agents" / "main" / "wire.jsonl"

    def message_of(self, row):
        kind = row.get("type")
        at = row.get("timestamp") or row.get("time")
        if kind == "agent.message.appended":
            envelope = row.get("message") or {}
            message = envelope.get("message") or {}
            return message.get("role"), message.get("content"), (envelope.get("meta") or {}).get("createdAt") or at
        if kind == "context.append_message":
            message = row.get("message") or {}
            if (message.get("origin") or {}).get("kind") not in (None, "user"):
                return None
            return message.get("role"), message.get("content"), at
        if kind == "context.append_loop_event":
            event = row.get("event") or {}
            if event.get("type") == "content.part" and (event.get("part") or {}).get("type") == "text":
                return "assistant", event["part"], at
        return None

    def read_info(self, path, journal):
        return read_json(path) or None

    def describe(self, path, data, head, rows, values, extra):
        sid = data.get("id") or path.parent.name
        values["id"] = sid if sid.startswith("session_") else "session_" + sid
        values["cwd"] = data.get("cwd") or data.get("workDir") or ""
        values["title"] = clean_title(data.get("title") or data.get("lastPrompt")) or values["title"]
        values["created"] = timestamp(data.get("createdAt"), values["created"])
        values["updated"] = timestamp(data.get("updatedAt"), values["updated"])
        extra["prompts"] = list(dict.fromkeys(prefix(r.get("input")) for r in rows if r.get("type") == "turn.prompt"))

    def valid_id(self, sid):
        return bool(UUID.fullmatch(sid.removeprefix("session_")))

    def valid_extra(self, entry):
        prompts = entry.get("prompts")
        return isinstance(prompts, list) and all(isinstance(p, str) for p in prompts)

    def indexes(self, cache):
        """Запросы, набранные человеком. Сессия, чьих запросов тут нет, запущена другим агентом."""
        def apply(values, row):
            value = prefix(row.get("content"))
            if value:
                values[value] = True
        history = set()
        for path in sorted((self.home() / "user-history").glob("*.jsonl")):
            history.update(update_index(path, cache, "kimi-history", apply, lambda value: value is True))
        return history

    def finish(self, session, entry, history):
        prompts = entry.get("prompts", [])
        session.auto = bool(prompts) and not any(p and p in history for p in prompts)
        session.by = "kimi -p" if session.auto else ""

    def headless(self, program, prompt, *, instructions, add_dirs, out_file):
        argv = [program, "--agent-file", str(instructions)]
        for directory in add_dirs:
            argv += ["--add-dir", str(directory)]
        return argv + ["--output-format", "stream-json", "--prompt", prompt], None

    def models(self):
        models, section, names = [], None, {}
        top = toml_top_level(self.home() / "config.toml")
        try:
            lines = (self.home() / "config.toml").read_text(encoding="utf-8").splitlines()
        except OSError:
            lines = []
        for line in lines:
            line = line.strip()
            match = re.fullmatch(r'\[models\."([^"]+)"\]', line)
            if line.startswith("["):
                section = match.group(1) if match else None
                if section:
                    models.append(section)
            elif section and line.startswith("display_name"):
                names[section] = line.split("=", 1)[1].strip().strip('"')
        return {"default_model": top.get("default_model", ""), "default_effort": "",
                "models": [{"id": m, "name": names.get(m, m), "efforts": [], "default_effort": ""} for m in models],
                "efforts": []}

    def choice_args(self, model, effort):
        return ["--model", model] if model else []

    def session_of(self, row):
        if row.get("type") == "session.resume_hint" and isinstance(row.get("session_id"), str):
            return row["session_id"]
        return ""

    def trace(self, row):
        from .base import clip
        role, items = row.get("role"), []
        if role == "assistant":
            reasoning = row.get("reasoning_content") or row.get("reasoning")
            if isinstance(reasoning, str) and reasoning.strip():
                items.append({"kind": "thinking", "text": clip(reasoning)})
            content = row.get("content")
            if isinstance(content, str) and content.strip():
                items.append({"kind": "text", "text": clip(content)})
            for call in row.get("tool_calls") or []:
                if isinstance(call, dict):
                    function = call.get("function") or {}
                    items.append({"kind": "tool", "tool": str(function.get("name", "")),
                                  "text": clip(function.get("arguments", ""), 600)})
        elif role == "tool":
            items.append({"kind": "result", "text": clip(row.get("content", ""), 1500)})
        return items

    def reply(self, program, session_id, text, *, instructions, add_dirs, out_file):
        argv = [program, "--agent-file", str(instructions)]
        for directory in add_dirs:
            argv += ["--add-dir", str(directory)]
        return argv + ["--session", session_id, "--output-format", "stream-json", "--prompt", text], None

    def stream_events(self, row):
        role = row.get("role")
        events = []
        if role == "assistant":
            calls = row.get("tool_calls")
            if isinstance(calls, list):
                for call in calls:
                    if not isinstance(call, dict):
                        continue
                    function = call.get("function") or {}
                    if function.get("name") == "Read":
                        events.append({"read": _read_path(function.get("arguments")), "call": str(call.get("id") or "")})
            content = row.get("content")
            if isinstance(content, str) and content.strip():
                events += [{"text": content}, {"answer": content}, {"finished": True}]
        elif role == "tool":
            events.append({"done": str(row.get("tool_call_id", ""))})
        return events


def _read_path(arguments) -> str | None:
    if isinstance(arguments, str):
        try:
            arguments = json.loads(arguments)
        except ValueError:
            return None
    if isinstance(arguments, dict):
        path = arguments.get("path") or arguments.get("file_path")
        if isinstance(path, str) and path:
            return path
    return None


AGENT = Kimi()
