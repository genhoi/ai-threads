"""Настройки: проверка значений и как они меняют команду продолжения."""

import json
import os
import shlex
from pathlib import Path

import pytest

from ai_threads import settings
from ai_threads.model import Session


def write(values):
    Path(os.environ["AI_THREADS_CONFIG"]).write_text(json.dumps(values), encoding="utf-8")


def test_skip_approvals_adds_the_agent_flag():
    manual = Session(tool="codex", id="abc", title="t", cwd="/tmp/$(touch pwned)", updated=0)
    auto = Session(tool="codex", id="abc", title="t", cwd="", updated=0, auto=True)
    assert manual.command_no_cd == "codex resume abc"
    write({"agents": {"codex": {"skip_approvals": True}, "claude": {"skip_approvals": True}}})
    assert manual.command_no_cd == "codex resume abc --dangerously-bypass-approvals-and-sandbox"
    assert manual.command == "cd -- '/tmp/$(touch pwned)' && " + manual.command_no_cd
    assert auto.command_no_cd == "codex exec resume abc --dangerously-bypass-approvals-and-sandbox"
    claude = Session(tool="claude", id="abc", title="t", cwd="", updated=0)
    assert claude.command_no_cd == "claude --resume abc --dangerously-skip-permissions"


def test_old_template_fields_are_rejected():
    with pytest.raises(ValueError, match="допустимы поля"):
        settings.validate({"agents": {"codex": {"resume": "codex resume {id}"}}})
    with pytest.raises(ValueError, match="true или false"):
        settings.validate({"agents": {"codex": {"skip_approvals": "yes"}}})


def test_configured_program_is_used_in_the_command(tmp_path):
    program = tmp_path / "{cwd} bin" / "codex"
    program.parent.mkdir()
    program.write_text("#!/bin/sh\n", encoding="utf-8")
    program.chmod(0o755)
    write({"agents": {"codex": {"program": str(program), "skip_approvals": True}}})
    manual = Session(tool="codex", id="abc", title="t", cwd="", updated=0)
    auto = Session(tool="codex", id="abc", title="t", cwd="", updated=0, auto=True)
    quoted = shlex.quote(str(program))
    assert manual.command_no_cd == f"{quoted} resume abc --dangerously-bypass-approvals-and-sandbox"
    assert auto.command_no_cd == f"{quoted} exec resume abc --dangerously-bypass-approvals-and-sandbox"


def test_zcode_has_no_command():
    write({"agents": {"zcode": {"skip_approvals": True}}})
    session = Session(tool="zcode", id="sess_1", title="t", cwd="/x", updated=0)
    assert session.command_no_cd == "" and session.command == ""
    assert session.resume_hint == "Продолжить можно только в приложении ZCode"


def test_broken_file_falls_back_to_defaults():
    Path(os.environ["AI_THREADS_CONFIG"]).write_text("{broken", encoding="utf-8")
    assert settings.effective()["search"]["timeout"] == 300
    assert settings.error()
