# xterm.js

Терминал на странице «Нити». Файлы взяты из npm без изменений, только переименованы:
у `.mjs` на Python 3.10 нет типа JavaScript, и браузер не загрузил бы их как модули.

| Файл | Пакет | Файл в пакете |
|---|---|---|
| `xterm.esm.js` | `@xterm/xterm` 6.0.0 | `lib/xterm.mjs` |
| `xterm.css` | `@xterm/xterm` 6.0.0 | `css/xterm.css` |
| `addon-fit.esm.js` | `@xterm/addon-fit` 0.11.0 | `lib/addon-fit.mjs` |

Лицензия MIT: тексты в `LICENSE` (`@xterm/xterm`) и `LICENSE-addon-fit` (`@xterm/addon-fit`).
