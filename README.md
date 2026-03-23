# Station Manager: database package

## Regenerating the database models

The build uses `sqlboiler` to generate the database models. To regenerate the models, each database
must be rebuilt separately, but both follow roughly the same steps.

### SQLite

1. Delete the existing development database file at `build/db/data.db`.
2. Run the `database/sqlite/example/main.go` file.
3. `cd database/sqlite`
4. `sqlboiler sqlite3`

### Postgres

1. Delete the existing development database file at `build/db/postgres_data` - this will require the use of `sudo`.
2. Start the Postgres Docker container.

   ```
    sudo systemctl start docker.service`
    docker-compose up
   ```

3. Run the `database/postgres/example/main.go` file.
4. `cd database/postgres`
5. `export PSQL_PASS=[some password]` - obviously replace `[some password]` with the correct password.
6. `sqlboiler psql`

## Adapters usage

This module uses the `adapters` package to map between `types` and DB models.
The new API is destination-first: `Into(dst, src)`. Generic helpers are provided in `database/helpers.go`.

Examples:

```go
// QSO insert (sqlite)
model, err := s.AdaptTypeToSqliteModelQso(qso)
if err != nil { /* handle */ }
err = model.Insert(ctx, h, boil.Infer())

// QSO fetch (postgres)
m, _ := pgmodels.FindQso(ctx, h, id)
out, err := s.AdaptPostgresModelToTypeQso(m)
```

## Context-aware CRUD

Each CRUD method has a `...Context(ctx, ...)` variant. If the caller doesn’t supply a deadline,
a default timeout is applied by the service. Transactions use `BeginTxContext` with a configurable timeout.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
qso, err := svc.InsertQsoContext(ctx, q)
```
