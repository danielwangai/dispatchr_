CREATE TABLE jobs (
    -- Public identifier for this job.
    id uuid PRIMARY KEY,
    -- Selects the handler that executes the job.
    type TEXT NOT NULL,
    -- Handler input data.
    payload jsonb NOT NULL,
    -- Current job lifecycle state.
    status text NOT NULL DEFAULT 'pending',
    -- Number of execution attempts made.
    attempts integer NOT NULL DEFAULT 0,
    -- Maximum allowed execution attempts.
    max_attempts integer NOT NULL DEFAULT 3,
    -- Most recent execution failure.
    last_error text,
    -- Time successful execution finished.
    completed_at timestamptz,
    -- Time the job was submitted.
    created_at timestamptz NOT NULL DEFAULT NOW(),
    -- Time the job was last changed.
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT jobs_status_check CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    CONSTRAINT jobs_attempts_check CHECK (attempts >= 0 AND max_attempts > 0)
);

