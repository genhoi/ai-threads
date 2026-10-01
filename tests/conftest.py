"""Общие фикстуры: заглушки CLI в PATH, простый Store, помощники ожидания."""

from __future__ import annotations

import os
import sys
import time
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
STUBS = Path(__file__).resolve().parent / "stubs"
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


class StubStore:
    """Заглушка Store с методами, которые вызывают summary.py и digest.py."""

    def __init__(self):
        self.summaries: dict[str, dict] = {}
        self.digests: dict[int, dict] = {}
        self.tails: dict[str, bool] = {}

    def set_summary(self, key: str, summary: dict) -> None:
        self.summaries[key] = summary

    def set_digest(self, days: int, digest: dict) -> None:
        self.digests[days] = digest

    def digest(self, days: int) -> dict | None:
        return self.digests.get(days)

    def set_tail(self, tail_id: str, done: bool) -> None:
        self.tails[tail_id] = done


@pytest.fixture(autouse=True)
def isolated_agents(tmp_path_factory, monkeypatch):
    """Тесты не видят настоящие папки агентов и файл настроек: каждая папка по умолчанию
    указывает на несуществующий путь, фикстура homes подменяет нужные."""
    from ai_threads.agents import AGENTS
    root = tmp_path_factory.mktemp("isolated")
    for agent in AGENTS.values():
        if agent.home_env:
            monkeypatch.setenv(agent.home_env, str(root / f"absent-{agent.id}"))
    monkeypatch.setenv("AI_THREADS_CONFIG", str(root / "settings.json"))
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)


@pytest.fixture
def stub_path(monkeypatch) -> Path:
    """PATH, где tests/stubs стоит впереди настоящих CLI."""
    monkeypatch.setenv("PATH", f"{STUBS}{os.pathsep}{os.environ.get('PATH', '')}")
    return STUBS


@pytest.fixture
def store() -> StubStore:
    return StubStore()


@pytest.fixture
def no_tools(monkeypatch, tmp_path) -> None:
    """PATH без CLI и пустой KIMI_CODE_HOME — ни одна модель недоступна."""
    monkeypatch.setenv("PATH", str(tmp_path))
    monkeypatch.setenv("KIMI_CODE_HOME", str(tmp_path))


def wait_finished(job, timeout: float = 15.0) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if job.finished:
            return
        time.sleep(0.02)
    raise AssertionError(f"задание {job.id} не завершилось за {timeout} с")


def all_events(job) -> list[dict]:
    events, done = job.events(0, 5.0)
    while not done:
        batch, done = job.events(events[-1]["n"], 5.0)
        events.extend(batch)
    return events


def wait_for(job, predicate, timeout: float = 10.0) -> list[dict]:
    """Дождаться события, удовлетворяющего predicate, и вернуть все события."""
    events, done = job.events(0, timeout)
    deadline = time.monotonic() + timeout
    while not any(predicate(e) for e in events):
        if done or time.monotonic() > deadline:
            raise AssertionError(f"событие не появилось: {events}")
        batch, done = job.events(events[-1]["n"] if events else 0,
                                 max(0.05, deadline - time.monotonic()))
        events.extend(batch)
    return events
