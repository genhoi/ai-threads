"""Выбор модели и уровня рассуждений: списки у CLI, флаги запуска, проверка в API."""

from __future__ import annotations

import json
import os
from pathlib import Path

import pytest

from ai_threads import runner
from ai_threads.agents import AGENTS
from ai_threads.jobs import Job


def home(agent_id, tmp_path, monkeypatch) -> Path:
    folder = tmp_path / agent_id
    folder.mkdir()
    monkeypatch.setenv(AGENTS[agent_id].home_env, str(folder))
    return folder


def test_codex_models_from_its_cache(tmp_path, monkeypatch):
    folder = home("codex", tmp_path, monkeypatch)
    (folder / "models_cache.json").write_text(json.dumps({"models": [
        {"slug": "gpt-new", "display_name": "GPT-New", "default_reasoning_level": "low",
         "supported_reasoning_levels": [{"effort": "low"}, {"effort": "max"}], "visibility": "list"},
        {"slug": "hidden", "display_name": "Hidden", "visibility": "hide"}]}), encoding="utf-8")
    (folder / "config.toml").write_text('model = "gpt-new"\nmodel_reasoning_effort = "max"\n\n[x]\nmodel = "no"\n',
                                        encoding="utf-8")
    models = AGENTS["codex"].models()
    assert models["default_model"] == "gpt-new" and models["default_effort"] == "max"
    assert models["models"] == [{"id": "gpt-new", "name": "GPT-New", "efforts": ["low", "max"], "default_effort": "low"}]


def test_kimi_models_from_config(tmp_path, monkeypatch):
    folder = home("kimi", tmp_path, monkeypatch)
    (folder / "config.toml").write_text('default_model = "kimi-code/a"\n\n[models."kimi-code/a"]\nmodel = "a"\n'
                                        'display_name = "Model A"\n\n[models."kimi-code/b"]\nmodel = "b"\n',
                                        encoding="utf-8")
    models = AGENTS["kimi"].models()
    assert models["default_model"] == "kimi-code/a"
    assert [(m["id"], m["name"]) for m in models["models"]] == [("kimi-code/a", "Model A"), ("kimi-code/b", "kimi-code/b")]


def test_claude_aliases_and_grok_command(stub_path, tmp_path, monkeypatch):
    folder = home("claude", tmp_path, monkeypatch)
    (folder / "settings.json").write_text('{"model": "sonnet"}', encoding="utf-8")
    claude = AGENTS["claude"].models()
    assert claude["default_model"] == "sonnet" and "opus" in [m["id"] for m in claude["models"]]
    assert claude["efforts"] == ["low", "medium", "high", "xhigh", "max"]
    grok = AGENTS["grok"].models()
    assert grok["default_model"] == "grok-test" and [m["id"] for m in grok["models"]] == ["grok-test", "grok-test-fast"]


@pytest.mark.parametrize("agent_id,flags", [
    ("codex", ["-m", "m1", "-c", 'model_reasoning_effort="high"']),
    ("claude", ["--model", "m1", "--effort", "high"]),
    ("grok", ["-m", "m1", "--reasoning-effort", "high"]),
    ("kimi", ["--model", "m1"]),
])
def test_choice_goes_right_after_the_program(stub_path, tmp_path, monkeypatch, agent_id, flags):
    seen = []
    monkeypatch.setattr(Job, "run_process", lambda self, argv, **kwargs: (seen.append(argv), (0, ""))[1])
    job = Job("x", "summary")
    runner.run(job, AGENTS[agent_id], "summary", "запрос", add_dirs=[], workdir=tmp_path, timeout=5,
               on_event=lambda event: None, model="m1", effort="high")
    assert seen[0][1:1 + len(flags)] == flags
    runner.run(job, AGENTS[agent_id], "summary", "запрос", add_dirs=[], workdir=tmp_path, timeout=5,
               on_event=lambda event: None)
    assert not ({"-m", "--model", "--effort", "--reasoning-effort"} & set(seen[1]))


def test_choice_is_validated():
    assert runner.choice("gpt-6.1-sol", "xhigh") == ("gpt-6.1-sol", "xhigh")
    assert runner.choice(None, None) == ("", "")
    for bad in (("a b", ""), ("", "x;y"), (5, "")):
        with pytest.raises(ValueError):
            runner.choice(*bad)
