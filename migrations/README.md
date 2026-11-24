# Database migrations

Create a migration with:

```bash
make migration-new NAME=create_jobs
```

This creates paired `*.up.sql` and `*.down.sql` files. Apply all pending
migrations with `make migrate-up`; inspect the applied version with
`make migrate-status`.
