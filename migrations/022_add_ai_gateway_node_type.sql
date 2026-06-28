-- AI_GATEWAY (AI Gateway Düğümü)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'AI_GATEWAY',
    'AI Gateway',
    'AI',
    'Gemini veya OpenAI modellerini kullanarak verileri analiz eder ve sonuçları sonraki adıma aktarır.',
    '{
      "type": "object",
      "properties": {
        "provider": {
          "type": "string",
          "title": "Sağlayıcı (Provider)",
          "default": "gemini",
          "enum": ["gemini", "openai"]
        },
        "api_key": {
          "type": "string",
          "title": "API Anahtarı (API Key)",
          "placeholder": "Boş bırakılırsa .env dosyasındaki anahtar kullanılır"
        },
        "model": {
          "type": "string",
          "title": "Model Adı (Model)",
          "default": "gemini-2.5-flash",
          "placeholder": "Örn: gemini-2.5-flash, gpt-4o-mini"
        },
        "prompt": {
          "type": "string",
          "title": "Analiz Talimatı (Prompt)",
          "placeholder": "Girdileri analiz etmek için kullanılacak talimat..."
        },
        "system_instruction": {
          "type": "string",
          "title": "Sistem Talimatı (System Instruction)",
          "placeholder": "Örn: Sen uzman bir veri analistisin."
        },
        "event_data": {
          "type": "array",
          "title": "Event Data (event_data)",
          "placeholder": "Örn: {{event.rows}}",
          "items": { "type": "object" }
        },
        "response_format": {
          "type": "string",
          "title": "Cevap Formatı (Response Format)",
          "default": "text",
          "enum": ["text", "json"]
        },
        "temperature": {
          "type": "number",
          "title": "Sıcaklık (Temperature)",
          "default": 0.2
        }
      }
    }'::jsonb,
    '{
      "type": "object"
    }'::jsonb,
    '{
      "type": "object",
      "properties": {
        "response": { "type": "string", "title": "Yapay Zeka Cevabı" },
        "structured_data": { "type": "object", "title": "Yapılandırılmış Veri (JSON)" },
        "status": { "type": "string", "title": "Durum" }
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for AI_GATEWAY
UPDATE def_node_types
SET ui_schema = '{"icon": "cpu", "color": "#8b5cf6", "bg_color": "#f5f3ff", "border_color": "#ddd6fe", "display_category": "AI"}'::jsonb,
    capabilities = '{"can_start_pipeline": false}'::jsonb
WHERE code = 'AI_GATEWAY';

-- Update config_schema for existing registration
UPDATE def_node_types
SET config_schema = '{
      "type": "object",
      "properties": {
        "provider": {
          "type": "string",
          "title": "Sağlayıcı (Provider)",
          "default": "gemini",
          "enum": ["gemini", "openai"]
        },
        "api_key": {
          "type": "string",
          "title": "API Anahtarı (API Key)",
          "placeholder": "Boş bırakılırsa .env dosyasındaki anahtar kullanılır"
        },
        "model": {
          "type": "string",
          "title": "Model Adı (Model)",
          "default": "gemini-2.5-flash",
          "placeholder": "Örn: gemini-2.5-flash, gpt-4o-mini"
        },
        "prompt": {
          "type": "string",
          "title": "Analiz Talimatı (Prompt)",
          "placeholder": "Girdileri analiz etmek için kullanılacak talimat..."
        },
        "system_instruction": {
          "type": "string",
          "title": "Sistem Talimatı (System Instruction)",
          "placeholder": "Örn: Sen uzman bir veri analistisin."
        },
        "event_data": {
          "type": "array",
          "title": "Event Data (event_data)",
          "placeholder": "Örn: {{event.rows}}",
          "items": { "type": "object" }
        },
        "response_format": {
          "type": "string",
          "title": "Cevap Formatı (Response Format)",
          "default": "text",
          "enum": ["text", "json"]
        },
        "temperature": {
          "type": "number",
          "title": "Sıcaklık (Temperature)",
          "default": 0.2
        }
      }
    }'::jsonb
WHERE code = 'AI_GATEWAY';
