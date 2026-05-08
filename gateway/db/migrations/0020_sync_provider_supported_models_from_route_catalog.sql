update provider_credentials pc
set supported_models = merged.supported_models
from (
    select
        pc_inner.id as provider_credential_id,
        coalesce(
            array_agg(distinct model order by model) filter (where model is not null and model <> ''),
            '{}'
        )::text[] as supported_models
    from provider_credentials pc_inner
    left join lateral (
        select unnest(coalesce(pc_inner.supported_models, '{}')) as model
        union
        select rc.requested_model as model
        from route_catalog rc
        where rc.provider_credential_id = pc_inner.id
          and rc.status = 'active'
    ) combined on true
    group by pc_inner.id
) merged
where pc.id = merged.provider_credential_id
  and coalesce(pc.supported_models, '{}') <> merged.supported_models;
