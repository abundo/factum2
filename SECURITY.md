# Security policy

Factum is self-hosted software. This policy covers vulnerabilities in the
code published in this repository. A problem that exists only in one
operator's deployment (host exposure, TLS, or secrets in that site's
config) belongs to that operator.

## Supported versions

Security fixes land on `main` and ship in the next tagged release. Only the
latest [GitHub release](https://github.com/abundo/factum2/releases) is
supported. Older tags are left as published.

## Reporting a vulnerability

Send a report in private:

1. Open a [private vulnerability report](https://github.com/abundo/factum2/security/advisories/new) on this repository.
2. If that form is unavailable, email [anders@abundo.se](mailto:anders@abundo.se).

Include the version or commit you tested, what an attacker can do, and how
to reproduce it. A proof of concept helps. Say whether the issue is already
public.

Public GitHub issues, discussions, and pull requests are readable by
everyone. Keep vulnerability details in the channels above.

## In scope

- The factum2 source in this repository: the web GUI, the CLI binaries, the
  worker hub, `install.py`, and the example configuration when a production
  install inherits the problem.
- How the software stores and hands out credentials and tokens (device
  access, hub tokens, database settings, integration secrets).

## Out of scope

- An operator's own deployment: host hardening, TLS at the reverse proxy,
  network exposure, and secrets in that site's config.
- Third-party products factum2 syncs with, including NetBox, Lime, DNS
  servers, Icinga, LibreNMS, Oxidized, Prometheus, and PostgreSQL.
- The local lab under `dev/`. Its default passwords are for a laptop
  compose stack.

Test against your own instance or a local lab. Research against a
deployment you do not operate needs that operator's permission.

## Disclosure

We aim to ship a fix within 90 days of a report we accept. Keep the report
private until a release with the fix is published, or until 90 days have
passed, whichever comes first. After that you may publish.

If a fix needs longer than 90 days we will say so and agree a new date. If
the issue is already public, or is being used against deployments, we may
publish an advisory and a release sooner.

We name reporters in the release notes when they want to be named.

## Safe harbor

Research that follows this policy is authorized against this project. If
you act in good faith, avoid privacy violations, avoid destroying data, and
avoid degrading a service someone else depends on, we will not pursue legal
action for that research.
