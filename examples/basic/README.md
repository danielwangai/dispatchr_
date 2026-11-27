# Basic Dispatchr Example

This example starts the HTTP API and one worker in the same process. It uses
PostgreSQL and registers two simulated handlers: `send_email` and
`generate_report`.

Run the local database and migrations first:

```sh
docker compose --env-file etc/config/config.local.env up -d postgres
make migrate-up
```

Start the example:

```sh
go run ./examples/basic
```

Submit a job from another terminal:

```sh
curl -X POST http://localhost:8080/jobs \
  -H 'Content-Type: application/json' \
  -d '{"type":"send_email","payload":{"to":"customer@example.com"}}'
```

The worker logs the simulated work and the job becomes `completed`. Use the
returned job ID to inspect it:

```sh
curl http://localhost:8080/jobs/<job-id>
```

Real applications register their own handlers in their own composition code;
these handlers exist only to make the example runnable.
