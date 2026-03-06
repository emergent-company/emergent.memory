-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.refresh_revision_counts() RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
      DECLARE 
        refresh_start TIMESTAMPTZ;
        refresh_end TIMESTAMPTZ;
        refresh_duration INTERVAL;
        is_populated BOOLEAN;
      BEGIN 
        refresh_start := clock_timestamp();
        
        -- Check if materialized view is populated
        SELECT COUNT(*) > 0 INTO is_populated
        FROM kb.graph_object_revision_counts LIMIT 1;
        
        -- Use CONCURRENTLY only if already populated, otherwise do regular refresh
        IF is_populated THEN
          REFRESH MATERIALIZED VIEW CONCURRENTLY kb.graph_object_revision_counts;
        ELSE
          REFRESH MATERIALIZED VIEW kb.graph_object_revision_counts;
        END IF;
        
        refresh_end := clock_timestamp();
        refresh_duration := refresh_end - refresh_start;
        
        RAISE NOTICE 'Revision counts refreshed in %', refresh_duration;
        
        RETURN (
          SELECT COUNT(*)::INTEGER
          FROM kb.graph_object_revision_counts
        );
      END;
      $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.refresh_revision_counts() RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
      DECLARE 
        refresh_start TIMESTAMPTZ;
        refresh_end TIMESTAMPTZ;
        refresh_duration INTERVAL;
      BEGIN 
        refresh_start := clock_timestamp();
        
        REFRESH MATERIALIZED VIEW CONCURRENTLY kb.graph_object_revision_counts;
        
        refresh_end := clock_timestamp();
        refresh_duration := refresh_end - refresh_start;
        
        RAISE NOTICE 'Revision counts refreshed in %', refresh_duration;
        
        RETURN (
          SELECT COUNT(*)::INTEGER
          FROM kb.graph_object_revision_counts
        );
      END;
      $$;
-- +goose StatementEnd
