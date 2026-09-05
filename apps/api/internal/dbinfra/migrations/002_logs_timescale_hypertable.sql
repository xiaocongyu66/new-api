-- applies-to: postgres
-- Convert `logs` into a TimescaleDB hypertable.
--
-- Preconditions (checked, not assumed):
--   * the timescaledb extension is installed in this database
--   * TIMESCALEDB_ENABLED=true (checked by the Go caller; this file is only
--     executed when the Go gate passed, so a hypertable is wanted)
--
-- The conversion takes an exclusive lock while create_hypertable moves existing
-- rows into chunks: measured 3.1 s on a 124k-row / 15-index replica at a 1-day
-- chunk interval. Plan the deploy stop accordingly.
--
-- Idempotent: the DO block checks whether logs is already a hypertable and
-- whether the primary key still needs widening, so re-running is a no-op.

DO $$
DECLARE
	pk_def text;
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
		RAISE NOTICE 'timescaledb extension not installed; skipping hypertable conversion';
		RETURN;
	END IF;

	IF EXISTS (SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name = 'logs') THEN
		RAISE NOTICE 'logs is already a hypertable; nothing to do';
		RETURN;
	END IF;

	-- Guards for manual psql runs: the app path always has the table (runner
	-- executes after AutoMigrate), but be explicit anyway.
	IF to_regclass('public.logs') IS NULL THEN
		RAISE NOTICE 'logs table does not exist; skipping hypertable conversion';
		RETURN;
	END IF;
	IF NOT EXISTS (
		SELECT 1 FROM pg_attribute
		WHERE attrelid = 'public.logs'::regclass AND attname = 'created_at' AND NOT attisdropped
	) THEN
		RAISE NOTICE 'logs has no created_at column; skipping hypertable conversion';
		RETURN;
	END IF;

	-- create_hypertable requires the partitioning column in every unique index,
	-- so widen PRIMARY KEY (id) -> (created_at, id) before converting. The id
	-- column keeps its own index for ORDER BY id pagination.
	SELECT pg_get_constraintdef(oid) INTO pk_def
	FROM pg_constraint
	WHERE conrelid = 'public.logs'::regclass AND contype = 'p';

	IF pk_def IS NOT NULL AND pk_def <> 'PRIMARY KEY (created_at, id)' THEN
		-- A single-column (id) key is widened; any other existing shape that
		-- already includes created_at is left untouched (the exact-string
		-- compare is stable for pg_get_constraintdef output on b-tree PKs).
		IF pk_def = 'PRIMARY KEY (id)' THEN
			ALTER TABLE logs DROP CONSTRAINT IF EXISTS logs_pkey;
			ALTER TABLE logs ADD PRIMARY KEY (created_at, id);
			CREATE INDEX IF NOT EXISTS idx_logs_id ON logs (id);
		ELSE
			RAISE NOTICE 'unexpected logs primary key (%); skipping conversion', pk_def;
			RETURN;
		END IF;
	ELSIF pk_def IS NULL THEN
		ALTER TABLE logs ADD PRIMARY KEY (created_at, id);
		CREATE INDEX IF NOT EXISTS idx_logs_id ON logs (id);
	END IF;

	PERFORM create_hypertable(
		'logs', 'created_at',
		chunk_time_interval => 86400::bigint,
		migrate_data => TRUE,
		if_not_exists => TRUE
	);

	-- created_at is a bigint epoch, so age-based policies need integer_now.
	EXECUTE 'CREATE OR REPLACE FUNCTION logs_created_at_now() RETURNS bigint
	         LANGUAGE SQL STABLE AS $fn$ SELECT extract(epoch FROM now())::bigint $fn$';
	PERFORM set_integer_now_func('logs', 'logs_created_at_now', replace_if_exists => TRUE);
END $$;
