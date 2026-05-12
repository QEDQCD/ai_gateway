alter table model_healthcheck_history
  drop constraint if exists model_healthcheck_history_route_id_fkey;
