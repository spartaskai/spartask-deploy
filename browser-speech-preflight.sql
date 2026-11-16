-- Read-only production preflight. Run before changing speech settings.
DO $$
DECLARE tenant RECORD; blocked BOOLEAN; definitions BIGINT;
BEGIN
  FOR tenant IN SELECT schema_name FROM public.tenants LOOP
    IF to_regclass(format('%I.speech_jobs', tenant.schema_name)) IS NOT NULL THEN
      EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.speech_jobs WHERE state IN (''PENDING'',''RUNNING'',''UNCERTAIN'') OR (state IN (''SUCCEEDED'',''FAILED'') AND delivered_at IS NULL))', tenant.schema_name) INTO blocked;
      IF blocked THEN
        RAISE EXCEPTION 'Speech jobs need draining or operator resolution in tenant %. Keep the previous release running.', tenant.schema_name;
      END IF;
    END IF;
    IF to_regclass(format('%I.run_executions', tenant.schema_name)) IS NOT NULL THEN
      EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.run_executions re JOIN %I.def_nodes n ON n.id=re.pipeline_node_id WHERE re.status=''PENDING'' AND n.type=''MEETING'')', tenant.schema_name, tenant.schema_name) INTO blocked;
      IF blocked THEN
        RAISE EXCEPTION 'Finish open meetings in tenant % before the browser speech transition.', tenant.schema_name;
      END IF;
      EXECUTE format('SELECT count(*) FROM %I.def_nodes WHERE type IN (''SPEECH_TO_TEXT'',''TEXT_TO_SPEECH'')', tenant.schema_name) INTO definitions;
      IF definitions > 0 THEN
        RAISE EXCEPTION 'Tenant % still has % speech executor definitions. Migrate those processes before retiring server speech.', tenant.schema_name, definitions;
      END IF;
    END IF;
  END LOOP;
END $$;
