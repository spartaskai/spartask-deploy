-- Alter table to add ui_schema column with default empty object
ALTER TABLE def_node_types 
ADD COLUMN IF NOT EXISTS ui_schema JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Seed default UI style configurations for existing node types
UPDATE def_node_types
SET ui_schema = '{"icon": "play", "color": "#22c55e", "bg_color": "#f0fdf4", "border_color": "#86efac", "display_category": "Utility"}'::jsonb
WHERE code = 'START_NODE';

UPDATE def_node_types
SET ui_schema = '{"icon": "square", "color": "#ef4444", "bg_color": "#fef2f2", "border_color": "#fca5a5", "display_category": "System"}'::jsonb
WHERE code = 'END_NODE';

UPDATE def_node_types
SET ui_schema = '{"icon": "zap", "color": "#eab308", "bg_color": "#fef9c3", "border_color": "#fef08a", "display_category": "Trigger"}'::jsonb
WHERE code = 'TRIGGER_NODE';

UPDATE def_node_types
SET ui_schema = '{"icon": "mail", "color": "#3b82f6", "bg_color": "#eff6ff", "border_color": "#bfdbfe", "display_category": "Communication"}'::jsonb
WHERE code = 'EMAIL_SENDER';

UPDATE def_node_types
SET ui_schema = '{"icon": "layout", "color": "#a855f7", "bg_color": "#faf5ff", "border_color": "#e9d5ff", "display_category": "UI"}'::jsonb
WHERE code = 'UI_RENDERER';

UPDATE def_node_types
SET ui_schema = '{"icon": "git-branch", "color": "#f97316", "bg_color": "#fff7ed", "border_color": "#ffedd5", "display_category": "Integration"}'::jsonb
WHERE code = 'EVENT_BUS';
