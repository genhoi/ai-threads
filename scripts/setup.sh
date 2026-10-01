#!/usr/bin/env bash
# Окружение для тестов: pytest и Playwright с Chromium и WebKit. Само приложение зависимостей не требует.
set -euo pipefail
cd "$(dirname "$0")/.."
if command -v uv >/dev/null; then
  uv venv --quiet --allow-existing --python python3 .venv
  uv pip install --quiet --python .venv/bin/python pytest playwright
else
  python3 -m venv .venv
  .venv/bin/python -m pip install --quiet pytest playwright
fi
.venv/bin/python -m playwright install chromium webkit
