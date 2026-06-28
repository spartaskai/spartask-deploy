-- Add API_SENDER (HTTP Client) node type
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'API_SENDER',
    'HTTP Client (API)',
    'INTEGRATION',
    'Dış servislere dinamik parametrelerle HTTP isteği gönderir.',
    -- Design-time UI configurations
    '{
      "type": "object",
      "properties": {
        "url": {
          "type": "string",
          "title": "API Endpoint URL",
          "placeholder": "https://api.example.com/users/{{payload.user_id}}"
        },
        "method": {
          "type": "string",
          "title": "HTTP Yöntemi",
          "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"],
          "default": "POST"
        },
        "headers": {
          "type": "object",
          "title": "HTTP Header Tanımları",
          "placeholder": "{\"Authorization\": \"Bearer {{global.token}}\"}"
        },
        "body_template": {
          "type": "object",
          "title": "İstek Gövdesi (JSON)",
          "placeholder": "{\"user_id\": \"{{payload.user_id}}\", \"action\": \"notify\"}"
        },
        "timeout_seconds": {
          "type": "integer",
          "title": "Zaman Aşımı (Saniye)",
          "default": 10
        }
      },
      "required": ["url", "method"]
    }'::jsonb,
    -- Runtime Inputs
    '{"type": "object"}',
    -- Runtime Outputs
    '{
      "type": "object",
      "properties": {
        "status_code": {"type": "integer"},
        "success": {"type": "boolean"},
        "response_body": {"type": "object"},
        "headers": {"type": "object"}
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for API_SENDER
UPDATE def_node_types
SET ui_schema = '{"icon": "globe", "color": "#f97316", "bg_color": "#fff7ed", "border_color": "#ffedd5", "display_category": "Integration"}'::jsonb
WHERE code = 'API_SENDER';
