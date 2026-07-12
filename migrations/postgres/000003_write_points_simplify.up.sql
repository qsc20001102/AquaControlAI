-- Write points use one explicit permission switch; keep the previous
-- effective permission before removing the redundant columns. The guard
-- keeps startup migrations safe when the service is restarted.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'write_points'
          AND column_name = 'enabled'
    ) THEN
        EXECUTE 'UPDATE write_points SET write_enabled = enabled AND write_enabled WHERE enabled IS NOT NULL';
        EXECUTE 'ALTER TABLE write_points DROP COLUMN enabled';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'write_points'
          AND column_name = 'readback_tolerance'
    ) THEN
        EXECUTE 'ALTER TABLE write_points DROP COLUMN readback_tolerance';
    END IF;
END $$;
