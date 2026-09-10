-- Demo dataset for pglight (runs once on first container init).
-- Exercises every explorer group: tables + FK, view, matview, function,
-- enum type, sequence defaults, indexes, CHECK/UNIQUE constraints, trigger.
CREATE TABLE authors (
  id         SERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  email      TEXT UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE books (
  id         SERIAL PRIMARY KEY,
  author_id  INT NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  price      NUMERIC(10,2) NOT NULL DEFAULT 0,
  meta       JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_books_author ON books(author_id);
CREATE INDEX idx_books_status ON books(status);

CREATE TYPE mood AS ENUM ('happy', 'neutral', 'sad');

CREATE TABLE reviews (
  id      SERIAL PRIMARY KEY,
  book_id INT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  rating  INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
  feeling mood NOT NULL DEFAULT 'neutral',
  note    TEXT
);

CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END $$;

CREATE TRIGGER trg_books_touch
  BEFORE UPDATE ON books
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE VIEW published_books AS
  SELECT b.id, b.title, a.name AS author
  FROM books b JOIN authors a ON a.id = b.author_id
  WHERE b.status = 'published';

CREATE MATERIALIZED VIEW author_stats AS
  SELECT a.id, a.name, count(b.id) AS book_count
  FROM authors a LEFT JOIN books b ON b.author_id = a.id
  GROUP BY 1, 2;

CREATE OR REPLACE FUNCTION book_count_by_status(s TEXT) RETURNS INT
LANGUAGE sql STABLE AS $$
  SELECT count(*)::int FROM books WHERE status = s
$$;

INSERT INTO authors(name, email) VALUES
  ('Nguyen Van A', 'a@example.com'),
  ('Tran Thi B',   'b@example.com'),
  ('Le Van C',     'c@example.com');

INSERT INTO books(author_id, title, status, price, meta) VALUES
  (1, 'Postgres co ban', 'published',  99000, '{"pages": 120}'),
  (1, 'SQL nang cao',    'draft',     149000, '{"pages": 300}'),
  (2, 'DataGrip tips',   'published',  79000, '{"pages": 80}'),
  (3, 'Docker cho dev',  'archived',   59000, '{"pages": 60}');

INSERT INTO reviews(book_id, rating, feeling, note) VALUES
  (1, 5, 'happy',   'rat hay'),
  (3, 4, 'neutral', 'ok');

REFRESH MATERIALIZED VIEW author_stats;
