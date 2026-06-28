-- Seed Yönetici Onay Formu in dynamic_forms table
INSERT INTO dynamic_forms (id, title, fields) VALUES (
  'da2c1df8-8686-4e56-8a7e-128a1c97a5b4',
  'Yönetici Onay Formu',
  '[
    {
      "id": "order_id",
      "type": "string",
      "label": "Sipariş ID (Arama / Filtreleme)",
      "required": false,
      "placeholder": "Sipariş ID girerek filtreleyin..."
    },
    {
      "id": "items",
      "type": "array",
      "label": "Sipariş Kalemleri (Dinamik Tablo)",
      "required": true
    },
    {
      "id": "notes",
      "type": "string",
      "label": "Yönetici Onay Notu",
      "widget": "textarea",
      "placeholder": "Onay veya red notunuzu buraya giriniz..."
    },
    {
      "id": "isApproved",
      "type": "boolean",
      "label": "Siparişi Onaylıyor musunuz?",
      "required": true,
      "widget": "radio"
    }
  ]'::jsonb
) ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, fields = EXCLUDED.fields;

