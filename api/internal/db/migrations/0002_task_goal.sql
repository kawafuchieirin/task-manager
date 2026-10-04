-- タスクのゴール（何ができたら完了か）。目標時間（estimated_min）とは別の、達成条件の文章。
-- 既存のタスクは空文字（未設定）になる。
ALTER TABLE tasks ADD COLUMN goal TEXT NOT NULL DEFAULT '' CHECK (length(goal) <= 500);
