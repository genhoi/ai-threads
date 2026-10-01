import json
from concurrent.futures import ThreadPoolExecutor

import pytest

from ai_threads.store import Store
from .a_support import key


@pytest.mark.parametrize("failure", ["dump", "fsync", "replace"])
def test_failed_write_preserves_disk_memory_and_retry(tmp_path, monkeypatch, failure):
    path = tmp_path / "state.json"
    store = Store(path)
    store.set_pin(key(), True)
    original = path.read_bytes()

    def fail(*args, **kwargs):
        if failure == "dump":
            args[1].write('{"version":')
        raise OSError("Искусственный сбой записи")

    with monkeypatch.context() as context:
        context.setattr("ai_threads.store." + ("json.dump" if failure == "dump" else "os." + failure), fail)
        with pytest.raises(OSError):
            store.set_name(key(), "Название")
        assert path.read_bytes() == original
        assert store.state()["names"] == {}
        assert not list(tmp_path.glob(".state-*"))
    store.set_name(key(), "Название")
    store.set_pin(key(), True)
    persisted = Store(path).state()
    assert persisted["pins"] == [key()] and persisted["names"] == {key(): "Название"}


def test_versions_merge_and_detached_copies(tmp_path):
    store = Store(tmp_path / "state.json")
    store.set_pin(key(), True)
    store.set_name(key(), "Раньше")
    store.import_({"version": 1, "pins": [key("claude")], "names": {key(): "Теперь"}})
    summary = {"title": "Сводка", "summary": "Проверка завершена", "related": [{"key": key("claude"), "why": "Одна тема"}]}
    store.import_({"version": 2, "pins": [key()], "names": {key("grok"): "Каталог"}, "done": [key()], "hidden": [key("grok")], "summaries": {key(): summary}, "digest_tails": {"tail-1": True}})
    store.set_digest(3, {"at": 1, "result": {}})
    state = store.state()
    assert state["pins"] == [key(), key("claude")]
    assert state["names"][key()] == "Теперь"
    assert state["done"] == [key()] and state["hidden"] == [key("grok")]
    assert "digests" not in store.export()
    state["pins"].clear()
    assert len(store.state()["pins"]) == 2
    assert Store(store.path).digest(3) == {"at": 1, "result": {}}
    store.migrate([key("kimi")], {key(): "Перенос"})
    assert store.state()["migrated_local_storage"]
    store.set_name(key(), None)
    assert key() not in store.state()["names"]


@pytest.mark.parametrize("bad_key", ["../file", "codex:../../file", "unknown:id", "codex:", "codex:a\n", "codex:другой", None, 12])
def test_invalid_keys_are_rejected(tmp_path, bad_key):
    store = Store(tmp_path / "state.json")
    for method in (store.set_pin, store.set_done, store.set_hidden):
        with pytest.raises(ValueError):
            method(bad_key, True)
    with pytest.raises(ValueError):
        store.import_({"version": 2, "pins": [key(), bad_key]})
    assert store.state()["pins"] == [] and not store.path.exists()


@pytest.mark.parametrize("data", [
    {"version": 3}, {"version": True}, {"version": 2, "names": {key(): ""}},
    {"version": 2, "names": {key(): " "}}, {"version": 2, "names": {key(): "x" * 201}},
    {"version": 2, "names": {"grok:../bad": "Название"}}, {"version": 2, "done": "codex:id"},
    {"version": 2, "hidden": ["kimi:bad/id"]},
    {"version": 2, "summaries": {key(): {"title": "Сводка", "summary": "Текст", "related": [{"key": "grok:../bad", "why": "Тема"}]}}},
])
def test_invalid_import_does_not_change_state(tmp_path, data):
    store = Store(tmp_path / "state.json")
    store.set_pin(key(), True)
    original = store.path.read_bytes()
    with pytest.raises(ValueError):
        store.import_(data)
    assert store.path.read_bytes() == original


def test_concurrent_writes_are_not_lost(tmp_path):
    store = Store(tmp_path / "state.json")
    with ThreadPoolExecutor(max_workers=8) as pool:
        list(pool.map(lambda n: store.set_pin(key(n=n), True), range(30)))
    assert set(Store(store.path).state()["pins"]) == {key(n=n) for n in range(30)}
    assert json.loads(store.path.read_text())["version"] == 2


def test_corrupt_state_is_not_overwritten(tmp_path):
    path = tmp_path / "state.json"
    path.write_text("{broken")
    with pytest.raises(ValueError):
        Store(path)
    assert path.read_text() == "{broken"
