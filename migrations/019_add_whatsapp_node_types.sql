-- 1. WHATSAPP_LISTENER (WhatsApp Tetikleyici / Dinleyici)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'WHATSAPP_LISTENER',
    'WhatsApp Dinleyicisi',
    'TRIGGER',
    'WhatsApp üzerinden gelen müşteri mesajlarını dinler ve süreci tetikler.',
    -- Tasarım anında UI'da istenecek konfigürasyon (RJSF için)
    '{
      "type": "object",
      "properties": {
        "webhook_path": { 
          "type": "string", 
          "title": "Webhook Yolu", 
          "default": "whatsapp-incoming" 
        },
        "verify_token": { 
          "type": "string", 
          "title": "Meta Doğrulama Tokeni (Verify Token)",
          "placeholder": "Meta Developer panelinde belirlediğiniz token" 
        }
      },
      "required": ["webhook_path"]
    }'::jsonb,
    -- Runtime Inputs (Giriş parametresi yok, süreci başlatır)
    '{"type": "object"}'::jsonb,
    -- Çalışma zamanında (runtime) üreteceği veri şeması (AI ve sonraki düğümlere aktarılacak)
    '{
      "type": "object",
      "properties": {
        "sender_phone": { "type": "string", "title": "Müşteri Telefon Numarası" },
        "message_text": { "type": "string", "title": "Gelen Mesaj" },
        "message_id": { "type": "string", "title": "Mesaj ID" },
        "sender_name": { "type": "string", "title": "Müşteri Adı" }
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for WHATSAPP_LISTENER
UPDATE def_node_types
SET ui_schema = '{"icon": "message-square", "color": "#25D366", "bg_color": "#e8fced", "border_color": "#a3e9b9", "display_category": "Trigger"}'::jsonb,
    capabilities = '{"can_start_pipeline": true}'::jsonb
WHERE code = 'WHATSAPP_LISTENER';


-- 2. WHATSAPP_SENDER (WhatsApp Mesaj Gönderici)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'WHATSAPP_SENDER',
    'WhatsApp Gönderici',
    'INTEGRATION',
    'Belirtilen telefon numarasına şablon uyumlu WhatsApp mesajı gönderir.',
    -- Tasarım anındaki dynamic form ayarları
    '{
      "type": "object",
      "properties": {
        "to_number": { 
          "type": "string", 
          "title": "Alıcı Telefon Numarası", 
          "placeholder": "{{payload.sender_phone}}" 
        },
        "message_body": { 
          "type": "string", 
          "title": "Mesaj İçeriği (Template / Değişken Destekli)", 
          "placeholder": "Merhaba {{payload.sender_name}}, siparişiniz alınmıştır!" 
        }
      },
      "required": ["to_number", "message_body"]
    }'::jsonb,
    '{"type": "object"}'::jsonb,
    -- Çalışma zamanı çıktıları
    '{
      "type": "object",
      "properties": {
        "success": { "type": "boolean", "title": "Başarı Durumu" },
        "message_id": { "type": "string", "title": "Mesaj API ID" }
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for WHATSAPP_SENDER
UPDATE def_node_types
SET ui_schema = '{"icon": "send", "color": "#128C7E", "bg_color": "#e0f2f1", "border_color": "#b2dfdb", "display_category": "Integration"}'::jsonb,
    capabilities = '{"can_start_pipeline": false}'::jsonb
WHERE code = 'WHATSAPP_SENDER';
