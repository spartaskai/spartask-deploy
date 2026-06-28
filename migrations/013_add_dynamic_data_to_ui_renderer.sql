-- Update config_schema for UI_RENDERER to include dynamic_data property
UPDATE def_node_types
SET config_schema = '{
  "type": "object",
  "properties": {
    "form_id": {
      "type": "string",
      "format": "uuid",
      "title": "Form ID"
    },
    "dynamic_data": {
      "type": "string",
      "title": "Dinamik Veri Bağlama (Dynamic Data Bind)",
      "placeholder": "{{event.payload}}"
    }
  },
  "required": ["form_id"]
}'::jsonb
WHERE code = 'UI_RENDERER';
