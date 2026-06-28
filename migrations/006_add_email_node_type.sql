-- Seed the EMAIL_SENDER node type into the registry
INSERT INTO def_node_types (
    code, 
    name, 
    category, 
    description, 
    config_schema, 
    input_schema, 
    output_schema, 
    execution_strategy
) VALUES (
    'EMAIL_SENDER',
    'Email Sender',
    'COMMUNICATION',
    'Sends plain text or HTML emails using global or node-specific SMTP configurations, supporting template placeholders.',
    '{
        "type": "object",
        "properties": {
            "to": { "type": "string", "title": "Recipient Email Template", "description": "Dynamic or static address (e.g. user@example.com or {{.email}})" },
            "subject": { "type": "string", "title": "Email Subject", "description": "Subject line supporting templates (e.g. Welcome {{.name}}!)" },
            "body": { "type": "string", "title": "Email Body Template", "description": "HTML or Plain Text template for the email body" },
            "smtp_host": { "type": "string", "title": "Custom SMTP Host", "description": "Override global SMTP host if specified" },
            "smtp_port": { "type": "integer", "title": "Custom SMTP Port", "description": "Override global SMTP port if specified" },
            "smtp_username": { "type": "string", "title": "Custom SMTP Username", "description": "Override global SMTP username if specified" },
            "smtp_password": { "type": "string", "title": "Custom SMTP Password", "description": "Override global SMTP password if specified" }
        },
        "required": ["to", "subject", "body"]
    }',
    '{
        "type": "object",
        "properties": {
            "email": { "type": "string", "description": "Passed dynamically by previous steps (if recipient is dynamic)" },
            "variables": { "type": "object", "description": "Key-value pair map to resolve placeholders inside templates" }
        }
      }',
    '{
        "type": "object",
        "properties": {
            "success": { "type": "boolean" },
            "message": { "type": "string" },
            "sent_at": { "type": "string" }
        }
    }',
    'IN_PROCESS'
) ON CONFLICT (code) DO NOTHING;
