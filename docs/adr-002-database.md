# ADR-002: PostgreSQL for persistence

**Status:** accepted
**Date:** 2026-08-14
**Supersedes:** the JSON-file store described in `backend/internal/matches/store.go`

## Context

Phase 1 stored everything in a single JSON file, rewritten in full under a mutex on every write. That was a deliberate trade for a POC with one user, and it held up fine.

Phase 2 breaks it. Demo analysis introduces data the file shape can't carry:

- **Results must outlive the demo.** Parsing a demo costs 30-120 seconds of CPU on a burstable VM. Doing that twice for the same match is the thing we are explicitly trying to avoid, so "has this been analysed?" has to be a cheap, reliable lookup.
- **The data is relational.** A player has matches; a match has analyses; an analysis has many highlights. Expressing that as nested JSON means rewriting a user's entire record to append one highlight.
- **Concurrent writers now exist.** A background analysis worker writes while HTTP handlers write. One file plus one mutex serialises the whole application behind the slowest writer.
- **Queries appear.** "Pending analyses", "highlights for this match", "every ace this player has" are all trivial in SQL and all full scans over a JSON blob.

A container was requested, which rules the embedded options out of contention regardless of their merits.

## Decision

**PostgreSQL 17**, as a container on the compose network, reached over the internal network only, with its own named volume.

The schema lives in `backend/internal/db/migrations/`, applied at boot by a small embedded runner (`embed.FS`, an advisory lock, and a `schema_migrations` table). No migration tool: the entire mechanism is about eighty lines and runs unattended on a machine nobody is watching, so being able to read all of it beats a dependency.

The caching decision is expressed as a constraint rather than as code:

```sql
UNIQUE (share_code, parser_version)
```

Re-requesting an analysis finds the existing row and returns it. Bumping `parser_version` is the deliberate way to force re-analysis when the detectors improve — which means the cache cannot go stale silently, because the thing that invalidates it is the same thing that changes the output.

## Why Postgres over the alternatives

**MySQL / MariaDB** would work. Postgres wins on the two things this schema actually leans on: `jsonb` with real indexing for per-highlight metadata (so a new detector isn't a migration), and richer constraint and index support. Neither is a knockout; Postgres is simply the better fit for a shape that is relational with a flexible tail.

**MongoDB** is the wrong shape. This data has foreign keys and wants them enforced — an orphaned highlight or an analysis pointing at a deleted match is corruption, not flexibility. The one genuinely schemaless part is a single column, which `jsonb` covers without giving up integrity everywhere else.

**SQLite** is, on the technical merits, arguably the better answer for one small VM: no extra container, no extra process, no memory overhead, and it would handle this workload for years. It was rejected because a containerised database was explicitly requested, and because it makes moving off this VM harder later. Worth recording honestly, since the RAM argument below is real.

**Azure Database for PostgreSQL** is the eventual destination if this outgrows the VM, and choosing Postgres now makes that a connection-string change rather than a rewrite. It is not worth its cost while a single container does the job.

## Consequences

- **Memory.** Postgres wants a few hundred MB, on a VM with 1 GB that already runs Caddy, two app containers and WUD. This is the change that forces the VM to grow — `Standard_B2als_v2` (4 GB, ~€25/month against ~€6) is the expected landing place.
- **Backups become a real task.** The JSON file could be copied. Now it is `pg_dump`, and the volume must survive VM rebuilds. Nothing automates this yet.
- **A password to manage.** `POSTGRES_PASSWORD` joins the `.env` on the VM. Note that Postgres only reads it when it first initialises the volume — changing it later does not change the database's password, which is a genuinely confusing failure mode.
- **The auth codes are still plaintext**, now in a column instead of a file. Moving to a database is the natural moment to encrypt them; this ADR does not do it, and that remains open.
- **One-way migration.** `ImportLegacyJSON` runs once, refuses if any player already exists, and there is no path back to the file store.
