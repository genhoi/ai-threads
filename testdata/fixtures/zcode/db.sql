-- Схема базы ZCode 0.16.9 (cli/db/db.sqlite) без лишних таблиц и синтетические данные.
-- 001 — ручная сессия, 002 — подагент, 003 — пустая заархивированная сессия без названия,
-- 004 — ответвление с названием по умолчанию, 005 — прогон workflow.
-- Строки вставлены не по порядку, у частей ответа 001 id идут наоборот: порядок задаёт sequence.

CREATE TABLE session (
  id text primary key,
  project_id text not null,
  workspace_id text,
  parent_id text,
  slug text not null,
  directory text not null,
  path text,
  title text not null,
  version text not null,
  share_url text,
  summary_additions integer,
  summary_deletions integer,
  summary_files integer,
  summary_diffs text,
  revert text,
  permission text,
  time_created integer not null,
  time_updated integer not null,
  time_compacting integer,
  time_archived integer,
  task_type text not null default 'interactive',
  title_source text not null default 'first_input'
    check(title_source in ('default', 'first_input', 'generated', 'custom')),
  title_message_id text,
  time_title_updated integer,
  trace_id text
);
CREATE TABLE message (
  id text primary key,
  session_id text not null references session(id) on delete cascade,
  time_created integer not null,
  time_updated integer not null,
  data text not null,
  sequence integer
);
CREATE TABLE part (
  id text primary key,
  message_id text not null references message(id) on delete cascade,
  session_id text not null,
  time_created integer not null,
  time_updated integer not null,
  data text not null,
  sequence integer
);
CREATE INDEX session_parent_idx on session(parent_id);
CREATE INDEX message_session_time_created_id_idx on message(session_id, time_created, id);
CREATE INDEX message_session_sequence_idx on message(session_id, sequence, time_created, id);
CREATE INDEX part_session_idx on part(session_id);
CREATE INDEX part_message_sequence_idx on part(message_id, sequence, time_created, id);
CREATE INDEX part_session_message_sequence_idx on part(session_id, message_id, sequence);

INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated,
                     time_archived, task_type, title_source) VALUES
  ('sess_00000000-0000-4000-8000-000000000005', 'prj_sample', 'sess_00000000-0000-4000-8000-000000000001',
   'workflow-run', '/home/user/projects/sample app', 'Прогон проверки', '0.16.9', 1790583000000, 1790583600000,
   NULL, 'workflow_child', 'first_input'),
  ('sess_00000000-0000-4000-8000-000000000004', 'prj_sample', 'sess_00000000-0000-4000-8000-000000000001',
   'fork-check', '/home/user/projects/sample app', 'New session - 2026-09-28T09:00:00.000Z', '0.16.9',
   1790586000000, 1790587800000, NULL, 'fork', 'default'),
  ('sess_00000000-0000-4000-8000-000000000003', 'prj_sample', NULL, 'empty', '/home/user/projects/sample app', '',
   '0.16.9', 1790582400000, 1790582400000, 1790590000000, 'interactive', 'first_input'),
  ('sess_00000000-0000-4000-8000-000000000002', 'prj_sample', 'sess_00000000-0000-4000-8000-000000000001',
   'subagent', '/home/user/projects/sample app', 'Подагент: поиск записи', '0.16.9', 1790582500000, 1790582600000,
   NULL, 'subagent_child', 'first_input'),
  ('sess_00000000-0000-4000-8000-000000000001', 'prj_sample', NULL, 'catalog', '/home/user/projects/sample app',
   'Проверить каталог', '0.16.9', 1790582400000, 1790589600000, NULL, 'interactive', 'generated');

-- Сессия 001: запрос, служебное напоминание, шаг с инструментом, скрытый ответ, ответ из двух частей
-- и пересказ после сжатия контекста.
INSERT INTO message (id, session_id, time_created, time_updated, data, sequence) VALUES
  ('msg_a6', 'sess_00000000-0000-4000-8000-000000000001', 1790589500000, 1790589500000,
   '{"role":"user","semantics":{"origin":"agent_runtime","kind":"compact_summary","uiVisibility":"hidden"},"summary":{"diffs":[]}}', 6),
  ('msg_a5', 'sess_00000000-0000-4000-8000-000000000001', 1790589000000, 1790589600000,
   '{"role":"assistant","agent":"zcode-agent","semantics":{"origin":"agent_runtime","kind":"assistant_response","uiVisibility":"visible"}}', 5),
  ('msg_a4', 'sess_00000000-0000-4000-8000-000000000001', 1790588000000, 1790588000000,
   '{"role":"assistant","visibility":"model-only","semantics":{"origin":"agent_runtime","kind":"assistant_response","uiVisibility":"hidden"}}', 4),
  ('msg_a3', 'sess_00000000-0000-4000-8000-000000000001', 1790583000000, 1790583100000,
   '{"role":"assistant","agent":"zcode-agent","semantics":{"origin":"agent_runtime","kind":"assistant_response","uiVisibility":"visible"}}', 3),
  ('msg_a2', 'sess_00000000-0000-4000-8000-000000000001', 1790582470000, 1790582470000,
   '{"role":"user","synthetic":true,"visibility":"model-only","semantics":{"origin":"agent_runtime","kind":"todo_reminder","uiVisibility":"hidden"}}', 2),
  ('msg_a1', 'sess_00000000-0000-4000-8000-000000000001', 1790582460000, 1790582460000,
   '{"role":"user","agent":"zcode-agent","semantics":{"origin":"real_user","kind":"user_prompt","uiVisibility":"visible"}}', 1);

INSERT INTO part (id, message_id, session_id, time_created, time_updated, data, sequence) VALUES
  ('prt_06', 'msg_a6', 'sess_00000000-0000-4000-8000-000000000001', 1790589500000, 1790589500000,
   '{"type":"text","text":"Пересказ переписки после сжатия контекста","synthetic":true}', 1),
  ('prt_05', 'msg_a5', 'sess_00000000-0000-4000-8000-000000000001', 1790589000000, 1790589600000,
   '{"type":"step-finish","reason":"stop"}', 4),
  ('prt_04', 'msg_a5', 'sess_00000000-0000-4000-8000-000000000001', 1790589000000, 1790589600000,
   '{"type":"text","text":"Запись найдена.","time":{"start":1790589000000,"end":1790589600000}}', 2),
  ('prt_03', 'msg_a5', 'sess_00000000-0000-4000-8000-000000000001', 1790589000000, 1790589600000,
   '{"type":"text","text":"Проверка завершена.","time":{"start":1790589000000,"end":1790589600000}}', 3),
  ('prt_02', 'msg_a5', 'sess_00000000-0000-4000-8000-000000000001', 1790589000000, 1790589000000,
   '{"type":"step-start"}', 1),
  ('prt_14', 'msg_a4', 'sess_00000000-0000-4000-8000-000000000001', 1790588000000, 1790588000000,
   '{"type":"text","text":"Скрытый ответ"}', 1),
  ('prt_13', 'msg_a3', 'sess_00000000-0000-4000-8000-000000000001', 1790583000000, 1790583100000,
   '{"type":"step-finish","reason":"tool-calls"}', 4),
  ('prt_12', 'msg_a3', 'sess_00000000-0000-4000-8000-000000000001', 1790583000000, 1790583100000,
   '{"type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"catalog.json"},"output":"Ответ инструмента"}}', 3),
  ('prt_11', 'msg_a3', 'sess_00000000-0000-4000-8000-000000000001', 1790583000000, 1790583000000,
   '{"type":"reasoning","text":"Рассуждение модели"}', 2),
  ('prt_10', 'msg_a3', 'sess_00000000-0000-4000-8000-000000000001', 1790583000000, 1790583000000,
   '{"type":"step-start"}', 1),
  ('prt_09', 'msg_a2', 'sess_00000000-0000-4000-8000-000000000001', 1790582470000, 1790582470000,
   '{"type":"text","text":"Напоминание о списке задач","synthetic":true}', 1),
  ('prt_08', 'msg_a1', 'sess_00000000-0000-4000-8000-000000000001', 1790582460000, 1790582460000,
   '{"type":"text","text":"Служебная вставка с содержимым файла","synthetic":true}', 2),
  ('prt_07', 'msg_a1', 'sess_00000000-0000-4000-8000-000000000001', 1790582460000, 1790582460000,
   '{"type":"text","text":"Найди запись в каталоге и проверь её название"}', 1);

-- Подагент 002 и прогон workflow 005: в каталоге их быть не должно.
INSERT INTO message (id, session_id, time_created, time_updated, data, sequence) VALUES
  ('msg_b1', 'sess_00000000-0000-4000-8000-000000000002', 1790582500000, 1790582500000,
   '{"role":"user","semantics":{"origin":"real_user","kind":"user_prompt","uiVisibility":"visible"}}', 1),
  ('msg_c1', 'sess_00000000-0000-4000-8000-000000000005', 1790583000000, 1790583000000,
   '{"role":"user","semantics":{"origin":"real_user","kind":"user_prompt","uiVisibility":"visible"}}', 1);
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data, sequence) VALUES
  ('prt_b1', 'msg_b1', 'sess_00000000-0000-4000-8000-000000000002', 1790582500000, 1790582500000,
   '{"type":"text","text":"Найди запись в каталоге"}', 1),
  ('prt_c1', 'msg_c1', 'sess_00000000-0000-4000-8000-000000000005', 1790583000000, 1790583000000,
   '{"type":"text","text":"Запусти проверку"}', 1);

-- Ответвление 004: уведомление об ответвлении скрыто, название берётся из первого запроса.
INSERT INTO message (id, session_id, time_created, time_updated, data, sequence) VALUES
  ('msg_d3', 'sess_00000000-0000-4000-8000-000000000004', 1790587800000, 1790587800000,
   '{"role":"assistant","semantics":{"origin":"agent_runtime","kind":"assistant_response","uiVisibility":"visible"}}', 3),
  ('msg_d2', 'sess_00000000-0000-4000-8000-000000000004', 1790586100000, 1790586100000,
   '{"role":"user","semantics":{"origin":"real_user","kind":"user_prompt","uiVisibility":"visible"}}', 2),
  ('msg_d1', 'sess_00000000-0000-4000-8000-000000000004', 1790586000000, 1790586000000,
   '{"role":"user","synthetic":true,"visibility":"model-only","semantics":{"origin":"system","kind":"fork_notice","uiVisibility":"hidden"}}', 1);
INSERT INTO part (id, message_id, session_id, time_created, time_updated, data, sequence) VALUES
  ('prt_d3', 'msg_d3', 'sess_00000000-0000-4000-8000-000000000004', 1790587800000, 1790587800000,
   '{"type":"text","text":"Ответвление проверено."}', 1),
  ('prt_d2', 'msg_d2', 'sess_00000000-0000-4000-8000-000000000004', 1790586100000, 1790586100000,
   '{"type":"text","text":"Продолжи проверку в ответвлении"}', 1),
  ('prt_d1', 'msg_d1', 'sess_00000000-0000-4000-8000-000000000004', 1790586000000, 1790586000000,
   '{"type":"text","text":"Сессия ответвлена от другой","synthetic":true}', 1);
