ALTER TABLE board_settings ADD COLUMN IF NOT EXISTS codegen_command TEXT;
ALTER TABLE board_settings ADD COLUMN IF NOT EXISTS codegen_args JSONB NOT NULL DEFAULT '[]'::jsonb;
