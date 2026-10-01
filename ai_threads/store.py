"""Пользовательские данные с атомарной записью и блокировкой потоков."""

import copy
import json
import os
import re
import tempfile
import threading
from pathlib import Path

from . import i18n

SESSION_ID = re.compile(r"^[A-Za-z0-9_.-]+$")

i18n.add({
    "store.bad_key": {"ru": "Неверный ключ сессии", "en": "Invalid session key"},
    "store.bad_name": {"ru": "Название должно содержать от 1 до 200 символов",
                       "en": "The name must be 1 to 200 characters"},
    "store.need_bool": {"ru": "Ожидается логическое значение", "en": "Expected true or false"},
    "store.bad_version": {"ru": "Поддерживаются данные версий 1 и 2", "en": "Only data versions 1 and 2 are supported"},
    "store.need_list": {"ru": "Поле {field} должно быть списком", "en": "Field {field} must be a list"},
    "store.need_object": {"ru": "Поле {field} должно быть объектом", "en": "Field {field} must be an object"},
    "store.bad_tails": {"ru": "Неверные отметки сводки", "en": "Invalid digest checkmarks"},
    "store.summary_object": {"ru": "Сводка должна быть объектом", "en": "The summary must be an object"},
    "store.summary_empty": {"ru": "Текст сводки не должен быть пустым", "en": "The summary text must not be empty"},
    "store.next_step": {"ru": "Следующий шаг должен быть строкой", "en": "The next step must be a string"},
    "store.related_list": {"ru": "Связанные сессии должны быть списком", "en": "Related sessions must be a list"},
    "store.bad_related": {"ru": "Неверная связанная сессия", "en": "Invalid related session"},
    "store.bad_days": {"ru": "Период должен быть 3, 4 или 5 дней", "en": "The period must be 3, 4 or 5 days"},
    "store.digest_object": {"ru": "Сводка должна быть объектом", "en": "The digest must be an object"},
    "store.bad_tail": {"ru": "Неверный пункт сводки", "en": "Invalid digest item"},
})


def validate_key(key) -> str:
    """Ключ вида `<агент>:<id сессии>`; агент — из реестра."""
    from .agents import AGENTS
    tool, _, sid = key.partition(":") if isinstance(key, str) else ("", "", "")
    if tool not in AGENTS or not SESSION_ID.fullmatch(sid):
        raise ValueError(i18n.t("store.bad_key"))
    return key


def _name(name) -> str:
    if not isinstance(name, str) or not name.strip() or len(name) > 200:
        raise ValueError(i18n.t("store.bad_name"))
    return name.strip()


def _boolean(value):
    if not isinstance(value, bool):
        raise ValueError(i18n.t("store.need_bool"))
    return value


def _defaults() -> dict:
    return {"version": 2, "pins": [], "names": {}, "summaries": {}, "done": [], "hidden": [],
            "digests": {}, "digest_tails": {}, "migrated_local_storage": False}


def _import_data(data) -> dict:
    if not isinstance(data, dict) or type(data.get("version")) is not int or data["version"] not in (1, 2):
        raise ValueError(i18n.t("store.bad_version"))
    result = {}
    for field in ("pins", "done", "hidden"):
        values = data.get(field, []) if data["version"] == 2 or field == "pins" else []
        if not isinstance(values, list):
            raise ValueError(i18n.t("store.need_list", field=field))
        result[field] = list(dict.fromkeys(validate_key(k) for k in values))
    names = data.get("names", {})
    if not isinstance(names, dict):
        raise ValueError(i18n.t("store.need_object", field="names"))
    result["names"] = {validate_key(k): _name(v) for k, v in names.items()}
    if data["version"] == 2:
        summaries = data.get("summaries", {})
        if not isinstance(summaries, dict):
            raise ValueError(i18n.t("store.need_object", field="summaries"))
        result["summaries"] = {validate_key(k): _summary(v) for k, v in summaries.items()}
        tails = data.get("digest_tails", {})
        if not isinstance(tails, dict) or any(not isinstance(k, str) or not k.strip() for k in tails):
            raise ValueError(i18n.t("store.bad_tails"))
        result["digest_tails"] = {k: _boolean(v) for k, v in tails.items()}
        if "migrated_local_storage" in data:
            result["migrated_local_storage"] = _boolean(data["migrated_local_storage"])
    return result


def _summary(summary) -> dict:
    if not isinstance(summary, dict):
        raise ValueError(i18n.t("store.summary_object"))
    result = copy.deepcopy(summary)
    result["title"] = _name(result.get("title"))
    if not isinstance(result.get("summary"), str) or not result["summary"].strip():
        raise ValueError(i18n.t("store.summary_empty"))
    if "next_step" in result and not isinstance(result["next_step"], str):
        raise ValueError(i18n.t("store.next_step"))
    if "closed" in result:
        _boolean(result["closed"])
    related = result.get("related", [])
    if not isinstance(related, list):
        raise ValueError(i18n.t("store.related_list"))
    for item in related:
        if not isinstance(item, dict) or not isinstance(item.get("why"), str):
            raise ValueError(i18n.t("store.bad_related"))
        validate_key(item.get("key"))
    return result


def validate_days(days):
    if type(days) is not int or days not in (3, 4, 5):
        raise ValueError(i18n.t("store.bad_days"))
    return days


class Store:
    def __init__(self, path: Path):
        self.path = Path(path)
        self._lock = threading.RLock()
        self._data = _defaults()
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
        except FileNotFoundError:
            return
        # Повреждённое пользовательское состояние не затирается пустым.
        self._data.update(_import_data(data))
        if data.get("version") == 2:
            digests = data.get("digests", {})
            if not isinstance(digests, dict):
                raise ValueError(i18n.t("store.need_object", field="digests"))
            self._data["digests"] = digests

    def _write(self, data):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        fd, name = tempfile.mkstemp(prefix=".state-", suffix=".tmp", dir=self.path.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as stream:
                json.dump(data, stream, ensure_ascii=False, allow_nan=False)
                stream.write("\n")
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(name, self.path)
        finally:
            if os.path.exists(name):
                os.unlink(name)

    def _change(self, change):
        with self._lock:
            data = copy.deepcopy(self._data)
            change(data)
            self._write(data)
            self._data = data
            return self.state()

    def state(self) -> dict:
        with self._lock:
            return copy.deepcopy({k: v for k, v in self._data.items() if k != "digests"})

    def _set_flag(self, field, key, value):
        validate_key(key)
        _boolean(value)

        def change(data):
            if value and key not in data[field]:
                data[field].append(key)
            elif not value and key in data[field]:
                data[field].remove(key)
        return self._change(change)

    def set_pin(self, key, pinned):
        return self._set_flag("pins", key, pinned)

    def set_done(self, key, done):
        return self._set_flag("done", key, done)

    def set_hidden(self, key, hidden):
        return self._set_flag("hidden", key, hidden)

    def set_name(self, key, name):
        validate_key(key)
        value = None if name is None or name == "" else _name(name)

        def change(data):
            if value is None:
                data["names"].pop(key, None)
            else:
                data["names"][key] = value
        return self._change(change)

    def set_summary(self, key: str, summary: dict):
        validate_key(key)
        value = _summary(summary)
        return self._change(lambda data: data["summaries"].__setitem__(key, value))

    def set_digest(self, days: int, digest: dict):
        validate_days(days)
        if not isinstance(digest, dict):
            raise ValueError(i18n.t("store.digest_object"))
        value = copy.deepcopy(digest)
        return self._change(lambda data: data["digests"].__setitem__(str(days), value))

    def digest(self, days: int) -> dict | None:
        validate_days(days)
        with self._lock:
            return copy.deepcopy(self._data["digests"].get(str(days)))

    def set_tail(self, tail_id: str, done: bool):
        if not isinstance(tail_id, str) or not tail_id.strip():
            raise ValueError(i18n.t("store.bad_tail"))
        _boolean(done)
        return self._change(lambda data: data["digest_tails"].__setitem__(tail_id, done))

    def export(self) -> dict:
        return self.state()

    def import_(self, data: dict):
        incoming = _import_data(data)

        def change(state):
            for field, value in incoming.items():
                if isinstance(value, list):
                    state[field] = list(dict.fromkeys(state[field] + value))
                elif isinstance(value, dict):
                    state[field].update(value)
                else:
                    state[field] = state[field] or value
        return self._change(change)

    def migrate(self, pins: list, names: dict):
        return self.import_({"version": 2, "pins": pins, "names": names, "migrated_local_storage": True})
