"""Реестр агентов. Новый агент — модуль в этой папке с объектом AGENT и строка в списке ниже."""

from . import claude, codex, cursor, grok, kimi, zcode
from .base import Agent

# Порядок списка — порядок в интерфейсе и в выборе модели для сводок по умолчанию.
_ORDER = [codex.AGENT, claude.AGENT, grok.AGENT, kimi.AGENT, zcode.AGENT, cursor.AGENT]
AGENTS: dict[str, Agent] = {agent.id: agent for agent in _ORDER}
# Агенты, которые умеют составлять сводки; первым по умолчанию берётся Kimi.
RUNNERS = tuple(agent_id for agent_id in ("kimi", "claude", "codex", "grok") if AGENTS[agent_id].runner)


def get(agent_id: str) -> Agent:
    return AGENTS[agent_id]


def active() -> list[Agent]:
    """Включённые агенты, у которых есть папка с сессиями."""
    return [agent for agent in AGENTS.values() if agent.enabled() and agent.home().is_dir()]
