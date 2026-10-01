"""Тесты ai_threads.jobs: события, повторное подключение, отмена, процессы."""

from __future__ import annotations

import os
import signal
import subprocess
import sys
import textwrap
import threading
import time
from pathlib import Path

from ai_threads.jobs import Job, Jobs, TAIL_CHARS

from .conftest import all_events, wait_finished


def test_emit_numbers_events_and_elapsed():
    job = Job("j1", "summary", "kimi:x")
    first = job.emit({"type": "progress", "step": 0})
    time.sleep(0.05)
    second = job.emit({"type": "result", "result": {}})
    assert first["n"] == 1 and second["n"] == 2
    assert second["elapsed"] >= first["elapsed"]
    events, done = job.events(0, 0.1)
    assert not done and len(events) == 2


def test_events_wait_for_new_and_done():
    job = Job("j2", "digest")
    job.emit({"type": "stage", "index": 0, "label": "Сбор"})
    got, done = job.events(0, 0.05)
    assert [e["n"] for e in got] == [1] and not done

    def later():
        time.sleep(0.1)
        job.emit({"type": "result", "result": {}})
        job._mark_done()

    threading.Thread(target=later, daemon=True).start()
    got, done = job.events(1, 5.0)  # переподключение с since=1
    assert [e["n"] for e in got] == [2]
    got, done = job.events(2, 5.0)
    assert got == [] and done


def test_events_timeout_returns_not_done():
    job = Job("j3", "summary", "kimi:x")
    got, done = job.events(0, 0.1)
    assert got == [] and not done


def test_duplicate_start_returns_running_job():
    jobs = Jobs()
    started = []
    release = threading.Event()

    def target(job):
        started.append(job.id)
        release.wait(5)

    first = jobs.start("summary:kimi:x", "summary", "kimi:x", target)
    second = jobs.start("summary:kimi:x", "summary", "kimi:x", target)
    assert first is second
    assert started == ["summary:kimi:x"]
    assert jobs.get("summary:kimi:x") is first
    assert [j.id for j in jobs.running()] == ["summary:kimi:x"]
    release.set()
    wait_finished(first)
    assert jobs.running() == []
    # после завершения с тем же id запускается новое задание
    third = jobs.start("summary:kimi:x", "summary", "kimi:x", lambda job: None)
    assert third is not first
    wait_finished(third)


def test_jobs_unexpected_error_becomes_error_event():
    jobs = Jobs()

    def boom(job):
        raise RuntimeError("сломалось")

    job = jobs.start("digest", "digest", None, boom)
    wait_finished(job)
    events = all_events(job)
    assert events[-1]["type"] == "error"
    assert "сломалось" in events[-1]["message"]


def test_run_process_collects_lines_and_tail(tmp_path):
    job = Job("p1", "summary")
    lines = []
    code, output = job.run_process(
        [sys.executable, "-c", "print('строка один'); print('строка два')"],
        cwd=tmp_path, timeout=10, on_line=lines.append)
    assert code == 0
    assert lines == ["строка один", "строка два"]
    assert "строка два" in output


def test_run_process_tail_strips_ansi_and_limits_size(tmp_path):
    job = Job("p2", "summary")
    script = (
        "import sys; sys.stderr.write('\\x1b[31m' + 'x' * 5000 + '\\x1b[0m'); "
        "sys.exit(2)")
    code, output = job.run_process([sys.executable, "-c", script],
                                   cwd=tmp_path, timeout=10)
    assert code == 2
    assert "\x1b" not in output
    assert len(output) <= TAIL_CHARS + 1
    assert output.endswith("x")


def _tree_script(marker: str) -> str:
    # маркеры пишутся только если процесс дожил до конца сна
    return textwrap.dedent(f"""
        import subprocess, sys, time
        child = subprocess.Popen([sys.executable, "-c",
            "import time; time.sleep(30); open({marker!r} + '.child', 'w')"])
        grand = subprocess.Popen([sys.executable, "-c",
            "import time; time.sleep(30); open({marker!r} + '.grand', 'w')"],
            start_new_session=True)
        print("CHILD", child.pid, flush=True)
        print("GRAND", grand.pid, flush=True)
        time.sleep(30)
        open({marker!r}, "w").write("alive")
    """)


def _pid_alive(pid: int) -> bool:
    """Процесс жив и не зомби. Убитый «внук» может остаться зомби, пока его не уберёт
    новый родитель: на раннерах CI это не всегда происходит сразу."""
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    try:
        stat = Path(f"/proc/{pid}/stat").read_text()
    except OSError:
        return False
    return stat[stat.rfind(")") + 2:].split()[0] != "Z"


def test_run_process_timeout_kills_whole_tree(tmp_path):
    job = Job("p3", "digest")
    pids = {}
    marker = tmp_path / "parent-alive"

    def on_line(line):
        for tag in ("CHILD", "GRAND"):
            if line.startswith(tag + " "):
                pids[tag] = int(line.split()[1])

    code, _ = job.run_process([sys.executable, "-c", _tree_script(str(marker))],
                              cwd=tmp_path, timeout=0.8, on_line=on_line)
    assert code == -9
    assert not marker.exists(), "родитель пережил таймаут"
    assert pids, "заглушка не сообщила PID потомков"
    for tag, pid in pids.items():
        assert not _pid_alive(pid), f"{tag} {pid} остался жив"


def test_cancel_kills_whole_tree(tmp_path):
    job = Job("p4", "digest")
    pids = {}
    marker = tmp_path / "parent-alive"

    def on_line(line):
        for tag in ("CHILD", "GRAND"):
            if line.startswith(tag + " "):
                pids[tag] = int(line.split()[1])

    def cancel_later():
        deadline = time.monotonic() + 5
        while "GRAND" not in pids and time.monotonic() < deadline:
            time.sleep(0.02)
        job.cancel()

    threading.Thread(target=cancel_later, daemon=True).start()
    code, _ = job.run_process([sys.executable, "-c", _tree_script(str(marker))],
                              cwd=tmp_path, timeout=30, on_line=on_line)
    assert job.cancelled
    assert not marker.exists(), "родитель пережил отмену"
    assert pids, "заглушка не сообщила PID потомков"
    for tag, pid in pids.items():
        assert not _pid_alive(pid), f"{tag} {pid} остался жив"


def test_cancel_before_events_marks_done():
    jobs = Jobs()
    job = jobs.start("once", "summary", None, lambda j: time.sleep(0.2))
    assert jobs.cancel("once")
    wait_finished(job)
    assert job.cancelled
    assert not jobs.cancel("once")  # уже закончено
    assert not jobs.cancel("never-started")
