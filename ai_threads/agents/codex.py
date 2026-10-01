"""Codex CLI: `$CODEX_HOME/sessions/**/rollout-*.jsonl`, названия в `session_index.jsonl`."""

import re

from ..sources.common import HEAD_BYTES, UUID, clean_title, json_rows, timestamp, update_index
from .base import JsonlAgent, first_title, shell_prompt


class Codex(JsonlAgent):
    id = "codex"
    name = "Codex"
    color = "#3b4859"
    icon = ('<svg viewBox="0 0 24 24" role="img" aria-label="Codex"><rect x="0" y="0" width="24" height="24" rx="6" '
            'fill="#3b4859"></rect><path d="M12 4.8l6.2 3.6v7.2L12 19.2l-6.2-3.6V8.4z" stroke="#ffffff" stroke-width="2" '
            'stroke-linejoin="round" fill="none"></path><circle cx="12" cy="12" r="1.9" fill="#ffffff"></circle></svg>')
    home_env = "CODEX_HOME"
    home_default = ".codex"
    programs = ("codex",)
    pattern = "sessions/**/rollout-*.jsonl"
    resume = "codex resume {id}"
    skip_flag = "--dangerously-bypass-approvals-and-sandbox"
    resume_auto = "codex exec resume {id}"
    runner = True
    answer_file = True

    def message_of(self, row):
        if row.get("type") != "response_item":
            return None
        payload = row.get("payload") or {}
        if isinstance(payload, dict) and payload.get("type") == "message":
            return payload.get("role"), payload.get("content"), row.get("timestamp")
        return None

    def read_info(self, path, journal):
        if not UUID.fullmatch(path.stem[-36:]):
            return None
        meta = next((r.get("payload") or {} for r in json_rows(journal, HEAD_BYTES)
                     if r.get("type") == "session_meta"), {})
        # Подагенты пишут в source объект с родителем; это не отдельные сессии.
        return None if isinstance(meta.get("source"), dict) else meta

    def describe(self, path, meta, head, rows, values, extra):
        values["id"] = path.stem[-36:]
        values["cwd"] = meta.get("cwd") or ""
        values["branch"] = (meta.get("git") or {}).get("branch") or ""
        values["created"] = timestamp(meta.get("timestamp"), values["created"])
        values["auto"] = meta.get("source") == "exec"
        values["by"] = "codex exec" if values["auto"] else "я"

    def valid_id(self, sid):
        return bool(UUID.fullmatch(sid))

    def indexes(self, cache):
        def apply(values, row):
            if isinstance(row.get("id"), str):
                title = clean_title(row.get("thread_name"))
                if title:
                    values[row["id"]] = title
        return update_index(self.home() / "session_index.jsonl", cache, "codex-index", apply,
                            lambda value: isinstance(value, str))

    def finish(self, session, entry, names):
        session.title = names.get(session.id) or first_title(entry)

    def headless(self, program, prompt, *, instructions, add_dirs, out_file):
        argv = [program, "exec", "--json", "-s", "read-only", "--skip-git-repo-check", "-o", str(out_file), "-"]
        return argv, shell_prompt(instructions, prompt)

    def stream_events(self, row):
        kind = row.get("type")
        if kind in ("item.started", "item.completed"):
            item = row.get("item") or {}
            item_id = str(item.get("id", ""))
            if item.get("type") == "command_execution":
                paths = _command_paths(str(item.get("command", "")))
                if kind == "item.started":
                    return [{"read": path, "call": item_id} for path in paths]
                return [{"done": item_id, "paths": paths}]
            if item.get("type") == "agent_message" and kind == "item.completed":
                text = item.get("text")
                if isinstance(text, str) and text.strip():
                    return [{"text": text}, {"answer": text}]
        elif kind == "turn.completed":
            usage = row.get("usage") or {}
            used = usage.get("input_tokens"), usage.get("output_tokens")
            events = []
            if all(isinstance(x, (int, float)) for x in used):
                events.append({"tokens": int(used[0]) + int(used[1]), "final": True})
            return events + [{"finished": True}]
        return []


def _command_paths(command: str) -> list[str]:
    seen, paths = set(), []
    for match in re.finditer(r"(/\S+)", command):
        path = match.group(1).rstrip("'\"")
        if path not in seen:
            seen.add(path)
            paths.append(path)
    return paths


AGENT = Codex()
