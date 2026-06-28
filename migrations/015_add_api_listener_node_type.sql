-- Add API_LISTENER (API Dinleyicisi) node type
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'API_LISTENER',
    'API Dinleyicisi (Listener)',
    'TRIGGER',
    'Dış dünyadan gelen HTTP isteklerini yakalayarak süreci başlatır.',
    -- Design-time UI configurations
    '{
      "type": "object",
      "properties": {
        "webhook_path": {
          "type": "string",
          "title": "Benzersiz Webhook Yolu",
          "placeholder": "stripe-payments"
        },
        "allowed_methods": {
          "type": "array",
          "title": "Kabul Edilen HTTP Yöntemleri",
          "items": {
            "type": "string",
            "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"]
          },
          "default": ["POST"]
        },
        "security_token": {
          "type": "string",
          "title": "Güvenlik Anahtarı (Token - Opsiyonel)",
          "placeholder": "Gelen isteklerin X-Webhook-Token başlığıyla doğrulanması için"
        }
      },
      "required": ["webhook_path"]
    }'::jsonb,
    -- Runtime Inputs (Starts the flow, no upstream inputs)
    '{"type": "object"}',
    -- Runtime Outputs (Exposes raw body, request headers, and query parameters)
    '{
      "type": "object",
      "properties": {
        "body": {"type": "object"},
        "headers": {"type": "object"},
        "query": {"type": "object"}
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for API_LISTENER
UPDATE def_node_types
SET ui_schema = '{"icon": "activity", "color": "#ef4444", "bg_color": "#fef2f2", "border_color": "#fca5a5", "display_category": "Trigger"}'::jsonb
WHERE code = 'API_LISTENER';
