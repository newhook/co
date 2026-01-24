-- +up
-- Add structured source fields to pr_feedback table
-- These replace the fragile Source string with explicit typed fields

-- Add source_type column (enum: ci, workflow, review_comment, issue_comment)
ALTER TABLE pr_feedback ADD COLUMN source_type TEXT;

-- Add source_name column (human-readable name extracted from source)
ALTER TABLE pr_feedback ADD COLUMN source_name TEXT;

-- Add context column (JSON for structured context data)
ALTER TABLE pr_feedback ADD COLUMN context TEXT;

-- Migrate existing data: parse "CI: x" -> source_type='ci', source_name='x'
UPDATE pr_feedback SET
    source_type = CASE
        WHEN source LIKE 'CI:%' THEN 'ci'
        WHEN source LIKE 'Workflow:%' THEN 'workflow'
        WHEN source LIKE 'Review:%' THEN 'review_comment'
        WHEN source LIKE 'Comment:%' THEN 'issue_comment'
        ELSE 'ci'
    END,
    source_name = TRIM(SUBSTR(source, INSTR(source, ':') + 1))
WHERE source_type IS NULL;

-- Add index for filtering by source_type
CREATE INDEX idx_pr_feedback_source_type ON pr_feedback(source_type);

-- +down
DROP INDEX IF EXISTS idx_pr_feedback_source_type;
ALTER TABLE pr_feedback DROP COLUMN context;
ALTER TABLE pr_feedback DROP COLUMN source_name;
ALTER TABLE pr_feedback DROP COLUMN source_type;
