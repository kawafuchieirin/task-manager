-- 日時はすべて UTC の RFC3339 文字列（例: 2026-10-03T12:00:00Z）で保存する。

CREATE TABLE tasks (
    id            INTEGER PRIMARY KEY,
    title         TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 100),
    description   TEXT    NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    status        TEXT    NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'doing', 'done')),
    estimated_min INTEGER CHECK (estimated_min >= 0),
    completed_at  TEXT,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
);

CREATE INDEX idx_tasks_status ON tasks (status);

CREATE TABLE time_entries (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    started_at TEXT    NOT NULL,
    ended_at   TEXT, -- NULL はタイマー計測中
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX idx_time_entries_task_id ON time_entries (task_id);

-- 計測中のタイマーはアプリ全体で1つまで（ended_at IS NULL の行を1行に制限する）。
CREATE UNIQUE INDEX uq_time_entries_running ON time_entries ((ended_at IS NULL)) WHERE ended_at IS NULL;

CREATE TABLE reflections (
    task_id          INTEGER PRIMARY KEY REFERENCES tasks (id) ON DELETE CASCADE,
    body             TEXT NOT NULL,
    learned_json     TEXT,
    not_learned_json TEXT,
    extract_status   TEXT NOT NULL DEFAULT 'pending' CHECK (extract_status IN ('pending', 'ok', 'failed')),
    updated_at       TEXT NOT NULL
);
