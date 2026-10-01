"""Поток событий `claude -p --output-format stream-json` и `grok --output-format
streaming-messages-json`: у обоих одинаковые записи assistant, user и result."""


def tool_path(block: dict) -> str | None:
    if block.get("name") not in ("Read", "read_file"):
        return None
    inp = block.get("input")
    if isinstance(inp, dict):
        for key in ("file_path", "target_file", "path"):
            value = inp.get(key)
            if isinstance(value, str) and value:
                return value
    return None


def wire_events(row: dict) -> list[dict]:
    kind = row.get("type")
    events = []
    if kind == "system" and row.get("subtype") == "thinking_tokens":
        used = row.get("estimated_tokens")
        if isinstance(used, (int, float)) and not isinstance(used, bool):
            events.append({"tokens": int(used), "final": False})
    elif kind == "assistant":
        for block in (row.get("message") or {}).get("content") or []:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "tool_use" and block.get("name") in ("Read", "read_file"):
                events.append({"read": tool_path(block), "call": str(block.get("id") or "")})
            elif block.get("type") == "text":
                text = block.get("text")
                if isinstance(text, str) and text.strip():
                    events.append({"text": text})
    elif kind == "user":
        for block in (row.get("message") or {}).get("content") or []:
            if isinstance(block, dict) and block.get("type") == "tool_result":
                events.append({"done": str(block.get("tool_use_id", ""))})
    elif kind == "result":
        usage = row.get("usage") or {}
        total = usage.get("input_tokens"), usage.get("output_tokens")
        if all(isinstance(x, (int, float)) for x in total):
            events.append({"tokens": int(total[0]) + int(total[1]), "final": True})
        result = row.get("result")
        if isinstance(result, str) and result.strip():
            events += [{"answer": result}, {"finished": True}]
    return events
