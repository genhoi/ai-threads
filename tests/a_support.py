"""Изолированные журналы для тестов исполнителя A."""

import shutil
from pathlib import Path

import pytest

FIXTURES = Path(__file__).parent / "fixtures"
TOOLS = ("codex", "claude", "grok", "kimi")


def sid(n=1):
    return f"00000000-0000-4000-8000-{n:012d}"


def key(tool="codex", n=1):
    return f"{tool}:{'session_' if tool == 'kimi' else ''}{sid(n)}"


@pytest.fixture
def homes(tmp_path, monkeypatch):
    root = tmp_path / "tools"
    shutil.copytree(FIXTURES, root)
    for tool, variable in zip(TOOLS, ("CODEX_HOME", "CLAUDE_HOME", "GROK_HOME", "KIMI_CODE_HOME")):
        monkeypatch.setenv(variable, str(root / tool))
    monkeypatch.setenv("AI_THREADS_DATA", str(tmp_path / "data"))
    return root
