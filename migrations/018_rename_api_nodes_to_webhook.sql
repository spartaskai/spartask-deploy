-- 1. WEBHOOK_LISTENER kaydını kopyala/ekle
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, ui_schema, capabilities, execution_strategy, is_active)
SELECT 
    'WEBHOOK_LISTENER', 
    'Webhook Dinleyicisi (Listener)', 
    category, 
    'Dış dünyadan gelen Webhook / HTTP isteklerini yakalayarak süreci başlatır veya devam ettirir.', 
    config_schema, 
    input_schema, 
    output_schema, 
    ui_schema, 
    capabilities, 
    execution_strategy, 
    is_active
FROM def_node_types 
WHERE code = 'API_LISTENER'
ON CONFLICT (code) DO NOTHING;

-- 2. WEBHOOK_SENDER kaydını kopyala/ekle
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, ui_schema, capabilities, execution_strategy, is_active)
SELECT 
    'WEBHOOK_SENDER', 
    'Webhook Göndericisi (Sender)', 
    category, 
    'Dış HTTP API/Webhook servislerine istek atar ve gelen yanıtı sürece aktarır.', 
    config_schema, 
    input_schema, 
    output_schema, 
    ui_schema, 
    capabilities, 
    execution_strategy, 
    is_active
FROM def_node_types 
WHERE code = 'API_SENDER'
ON CONFLICT (code) DO NOTHING;

-- 3. def_nodes (tasarım şeması) referanslarını güncelle
UPDATE def_nodes SET type = 'WEBHOOK_LISTENER' WHERE type = 'API_LISTENER';
UPDATE def_nodes SET type = 'WEBHOOK_SENDER' WHERE type = 'API_SENDER';

-- 4. Eski API_LISTENER ve API_SENDER kayıtlarını güvenle sil
DELETE FROM def_node_types WHERE code IN ('API_LISTENER', 'API_SENDER');
