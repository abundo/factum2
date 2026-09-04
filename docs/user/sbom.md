---
title: Software bill of materials
order: 90
---

# Software bill of materials

A Factum **release** records the third-party Go modules and GUI npm
packages it was built with. That inventory is generated when the
`factum2-web` release binary is compiled (`make release`, or a GitHub
`v*` tag) and ships inside this Documentation page, so it matches the
binaries you are logged into.

This page is the placeholder shown on GitHub Pages and in a development
binary (`go run`, `make factum2-web`). Those are not a stamped release.
On an installed release, this same nav entry lists every Go module in
the module graph and every npm package locked for the GUI.

The same inputs live in the source tree: `go.mod` / `go.sum` and
`web/frontend/package-lock.json`.
