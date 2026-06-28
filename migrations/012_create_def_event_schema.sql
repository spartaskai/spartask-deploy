-- Create the Event Schema Registry table
CREATE TABLE IF NOT EXISTS def_event_schema (
    event_name VARCHAR(255) PRIMARY KEY,
    payload_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Seed a default standard event (OrderCreated)
INSERT INTO def_event_schema (event_name, payload_schema)
VALUES (
    'OrderCreated', 
    '{
        "type": "object",
        "properties": {
            "order_id": { "type": "string", "description": "Sipariş benzersiz kimliği" },
            "amount": { "type": "number", "description": "Sipariş toplam tutarı" },
            "customer_email": { "type": "string", "description": "Müşteri e-posta adresi" }
        }
    }'::jsonb
) ON CONFLICT (event_name) DO NOTHING;
