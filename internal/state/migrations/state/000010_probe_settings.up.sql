UPDATE system_config SET config_json = json_patch(
  '{"probe_enabled":true,"probe_concurrency":32,"probe_timeout":"15s","keepalive_enabled":false,"keepalive_interval":"5m","recovery_probe_interval":"1m","recovery_probe_max_interval":"1h","recovery_success_threshold":1,"node_validity_ttl":"2h","egress_attributes_enabled":true,"egress_attributes_provider":"check-host","egress_attributes_url":"https://api.check-host.cc/myinfo","egress_attributes_interval":"24h"}',
  config_json
);
