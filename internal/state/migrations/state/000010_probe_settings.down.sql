UPDATE system_config SET config_json = json_remove(config_json,
  '$.probe_enabled','$.probe_concurrency','$.probe_timeout','$.keepalive_enabled',
  '$.keepalive_interval','$.recovery_probe_interval','$.recovery_probe_max_interval',
  '$.recovery_success_threshold','$.node_validity_ttl','$.egress_attributes_enabled',
  '$.egress_attributes_provider','$.egress_attributes_url','$.egress_attributes_interval'
);
