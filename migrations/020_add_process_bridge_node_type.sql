-- PROCESS_BRIDGE (Süreç Köprüsü Düğümü)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'PROCESS_BRIDGE',
    'Process Bridge',
    'INTEGRATION',
    'Bridges two processes by starting a target pipeline, emitting a trigger event immediately, and dynamic final event propagation.',
    '{
      "type": "object",
      "properties": {
        "target_pipeline_id": { 
          "type": "string", 
          "title": "Hedef Süreç (Pipeline ID)", 
          "placeholder": "Seçilecek olan alt sürecin UUID formatındaki IDsi" 
        },
        "trigger_event_name": { 
          "type": "string", 
          "title": "Tetiklenme Anında Fırlatılacak Olay (Event Name)", 
          "default": "SüreçTetiklendi" 
        }
      },
      "required": ["target_pipeline_id"]
    }'::jsonb,
    '{"type": "object"}'::jsonb,
    '{
      "type": "object",
      "properties": {
        "sub_process_instance_id": { "type": "string", "title": "Başlatılan Süreç Örneği ID" },
        "status": { "type": "string", "title": "Durum" }
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for PROCESS_BRIDGE
UPDATE def_node_types
SET ui_schema = '{"icon": "git-merge", "color": "#7c3aed", "bg_color": "#f3e8ff", "border_color": "#d8b4fe", "display_category": "Integration"}'::jsonb,
    capabilities = '{"can_start_pipeline": false}'::jsonb
WHERE code = 'PROCESS_BRIDGE';
