-- Update config_schema for API_LISTENER to support starts_pipeline and correlation_key
UPDATE def_node_types
SET config_schema = '{
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
    },
    "starts_pipeline": {
      "type": "boolean",
      "title": "Süreci Başlatır mı?",
      "description": "Evet ise yeni süreç başlatır, hayır ise askıdaki bir süreci devam ettirir.",
      "default": true
    },
    "correlation_key": {
      "type": "string",
      "title": "Korelasyon Değişken Adı (Örn: order_id)",
      "description": "Gelen istek gövdesindeki bu alan ile sürecin global contextindeki aynı alan eşleştirilir.",
      "placeholder": "order_id"
    }
  },
  "required": ["webhook_path"]
}'::jsonb
WHERE code = 'API_LISTENER';
