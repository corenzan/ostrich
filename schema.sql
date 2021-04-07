CREATE TABLE IF NOT EXISTS documents (
	path text NOT NULL PRIMARY KEY,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	contents jsonb
);
CREATE INDEX ON documents (path text_pattern_ops);