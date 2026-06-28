-- Kullanıcılar Tablosu
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    full_name VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Pipeline'lara sahiplik ekliyoruz
-- (Mevcut verileri bozmamak için şimdilik NULL olabilir, ilerde NOT NULL yaparız)
ALTER TABLE def_pipelines 
ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);

-- İndex (Hızlı login için)
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);