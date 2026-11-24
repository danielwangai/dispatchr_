CREATE INDEX jobs_pending_created_at_idx ON jobs (created_at, id)
WHERE
    status = 'pending';

