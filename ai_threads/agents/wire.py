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


def _content_text(content) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(part.get("text", "") for part in content if isinstance(part, dict))
    return ""


def wire_trace(row: dict) -> list[dict]:
    """Журнал из потока claude/grok: модель, рассуждение, текст, вызовы и результаты инструментов."""
    from .base import clip
    kind = row.get("type")
    items = []
    if kind == "system" and row.get("subtype") == "init" and isinstance(row.get("model"), str):
        items.append({"kind": "model", "text": row["model"]})
    elif kind == "assistant":
        for block in (row.get("message") or {}).get("content") or []:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "thinking" and block.get("thinking"):
                items.append({"kind": "thinking", "text": clip(block["thinking"])})
            elif block.get("type") == "text" and block.get("text"):
                items.append({"kind": "text", "text": clip(block["text"])})
            elif block.get("type") == "tool_use":
                items.append({"kind": "tool", "tool": str(block.get("name", "")), "text": clip(block.get("input", ""), 600)})
    elif kind == "user":
        for block in (row.get("message") or {}).get("content") or []:
            if isinstance(block, dict) and block.get("type") == "tool_result":
                items.append({"kind": "result", "text": clip(_content_text(block.get("content")), 1500)})
    elif kind == "result":
        usage = row.get("usage") or {}
        parts = [f"in {usage['input_tokens']}" if isinstance(usage.get("input_tokens"), int) else "",
                 f"out {usage['output_tokens']}" if isinstance(usage.get("output_tokens"), int) else "",
                 f"${row['total_cost_usd']:.2f}" if isinstance(row.get("total_cost_usd"), (int, float)) else ""]
        if any(parts):
            items.append({"kind": "usage", "text": " · ".join(p for p in parts if p)})
    return items


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
