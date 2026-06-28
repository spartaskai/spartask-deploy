-- Migration: Add transition_port column to def_node_publications
ALTER TABLE def_node_publications 
ADD COLUMN IF NOT EXISTS transition_port VARCHAR(100) NOT NULL DEFAULT 'success';
