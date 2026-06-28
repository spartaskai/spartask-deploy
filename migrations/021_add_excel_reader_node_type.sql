-- EXCEL_READER (Excel Okuyucu Düğümü)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'EXCEL_READER',
    'Excel Okuyucu',
    'UTILITY',
    'Excel dosyasını (yerel yol, URL veya Base64 formatında) okuyup satırlarını JSON array olarak sonraki adıma aktarır.',
    '{
      "type": "object",
      "properties": {
        "file_path": { 
          "type": "string", 
          "title": "Excel Dosya Yolu (Local Path)", 
          "placeholder": "Örn: C:\\data\\excel.xlsx" 
        },
        "file_url": { 
          "type": "string", 
          "title": "Excel İndirme Bağlantısı (URL)", 
          "placeholder": "Örn: https://example.com/data.xlsx" 
        },
        "file_base64": { 
          "type": "string", 
          "title": "Excel Base64 Verisi", 
          "placeholder": "data:application/...base64,..." 
        },
        "sheet_name": { 
          "type": "string", 
          "title": "Çalışma Sayfası Adı (Sheet Name)", 
          "placeholder": "Boş bırakılırsa ilk sayfa okunur" 
        },
        "has_header": { 
          "type": "boolean", 
          "title": "Başlık Satırı Var mı? (Has Header)", 
          "default": true 
        }
      }
    }'::jsonb,
    '{
      "type": "object",
      "properties": {
        "file_path": { "type": "string" },
        "file_url": { "type": "string" },
        "file_base64": { "type": "string" },
        "sheet_name": { "type": "string" }
      }
    }'::jsonb,
    '{
      "type": "object",
      "properties": {
        "rows": { 
          "type": "array", 
          "title": "Satırlar",
          "items": { "type": "object" } 
        },
        "row_count": { "type": "integer", "title": "Satır Sayısı" },
        "sheet_name": { "type": "string", "title": "Okunan Sayfa" }
      }
    }'::jsonb,
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;

-- Seed default UI style configurations for EXCEL_READER
UPDATE def_node_types
SET ui_schema = '{"icon": "file-text", "color": "#16a34a", "bg_color": "#dcfce7", "border_color": "#86efac", "display_category": "Utility"}'::jsonb,
    capabilities = '{"can_start_pipeline": false}'::jsonb
WHERE code = 'EXCEL_READER';
