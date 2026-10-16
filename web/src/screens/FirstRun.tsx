import { useState } from 'preact/hooks';
import { AgentBadge, Button, Icon, IconButton, toast } from '../components';
import { AGENTS, type AgentId } from '../model';
import { copyText } from '../app/actions';
import { launchableAgents, useApp } from '../app/state';
import { refreshEnv } from './useSettingsDraft';

/** Где ставить агентов: страницы установки у самих агентов */
export const INSTALL_PAGES: Record<Exclude<AgentId, 'zcode'>, string> = {
  claude: 'https://code.claude.com/docs/en/setup',
  codex: 'https://developers.openai.com/codex/cli',
  grok: 'https://x.ai/cli',
  kimi: 'https://www.kimi.com/code',
  cursor: 'https://cursor.com/cli',
};

const AGENT_ORDER: Exclude<AgentId, 'zcode'>[] = ['claude', 'codex', 'grok', 'kimi', 'cursor'];

const TMUX_INSTALL = [
  { os: 'macOS', cmd: 'brew install tmux' },
  { os: 'Linux', cmd: 'sudo apt install tmux' },
];

/** Первый запуск: нет tmux или ни одного агента. Что поставить и «Проверить снова» */
export function FirstRun() {
  const tmux = useApp((s) => !!s.env?.tmux);
  const agents = useApp((s) => launchableAgents(s).length > 0);
  // Агенты стоят, но все выключены в настройках: ставить нечего, нужно включить
  const allOff = useApp((s) => !launchableAgents(s).length && s.agents.some((a) => a.id !== 'zcode' && !!a.program));
  const [checking, setChecking] = useState(false);

  let title = allOff ? 'Осталось включить агента' : 'Осталось установить агента';
  if (!tmux) title = agents ? 'Осталось установить tmux' : allOff ? 'Осталось установить tmux и включить агента' : 'Осталось установить tmux и агента';
  return (
    <main class="firstrun">
      <div class="firstrun__inner">
        <h1 class="firstrun__title">{title}</h1>
        <p class="firstrun__text">
          В tmux агенты продолжают работу, даже если закрыть страницу. Агент нужен хотя бы один: «Нить» найдёт его сама.
        </p>

        {!tmux && (
          <section class="fr-card fr-card--tmux on-surface-1" aria-label="tmux">
            <h2 class="fr-card__head">
              <span class="smark smark--error" aria-hidden="true" />
              tmux не найден
            </h2>
            <div class="fr-cmds">
              {TMUX_INSTALL.map((x) => (
                <div class="fr-cmd" key={x.os}>
                  <span class="fr-cmd__os">{x.os}</span>
                  <div class="fr-cmd__box">
                    <code class="fr-cmd__text">{x.cmd}</code>
                    <IconButton icon="content_copy" label="Скопировать" size={32} onClick={() => void copyText(x.cmd, 'Команда скопирована')} />
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {!agents && allOff && (
          <section class="fr-card fr-card--agents on-surface-1" aria-label="Агенты">
            <h2 class="fr-card__head">
              <span class="smark smark--error" aria-hidden="true" />
              Все агенты выключены в настройках
            </h2>
            <div class="fr-agent">
              <span class="fr-agent__name fr-agent__name--note">Включите хотя бы одного: Настройки → Агенты → «Показывать в списке».</span>
              <a class="btn btn--quiet btn--32 btn--lead fr-agent__link" href="#/settings">
                <Icon name="settings" size={16} />
                Открыть настройки
              </a>
            </div>
          </section>
        )}

        {!agents && !allOff && (
          <section class="fr-card fr-card--agents on-surface-1" aria-label="Агенты">
            <h2 class="fr-card__head">
              <span class="smark smark--error" aria-hidden="true" />
              Агенты не найдены
            </h2>
            {AGENT_ORDER.map((id) => (
              <div class="fr-agent" key={id}>
                <AgentBadge agent={id} size={28} decorative />
                <span class="fr-agent__name">{AGENTS[id].name}</span>
                <a class="btn btn--quiet btn--32 btn--lead fr-agent__link" href={INSTALL_PAGES[id]} target="_blank" rel="noopener noreferrer">
                  <Icon name="open_in_new" size={16} />
                  Как установить
                  <span class="sr-only">{AGENTS[id].name}</span>
                </a>
              </div>
            ))}
          </section>
        )}

        {/* Агенты только выключены: ставить нечего, включённый агент появится сразу */}
        {!(tmux && allOff) && (
          <div class="firstrun__foot">
            <Button
              variant="primary"
              icon="restart_alt"
              loading={checking}
              onClick={async () => {
                setChecking(true);
                const error = await refreshEnv();
                if (error) toast.show({ kind: 'error', title: 'Не получилось', text: error });
                setChecking(false);
              }}
            >
              Проверить снова
            </Button>
            <span class="firstrun__note">«Нить» ищет программы в PATH. После установки нажмите «Проверить снова».</span>
          </div>
        )}
      </div>
    </main>
  );
}
