# Reverse proxy

`factum2-web` speaks HTTP only. Run the primary behind a reverse proxy
that terminates TLS — Caddy, Traefik, Apache, or equivalent. The Go
process binds to loopback (`web.bind` in `/etc/factum2/factum2.yaml`;
the example uses `127.0.0.1:8091`) so browsers and NetBox never reach it
directly.

Production login cookies set `Secure` (except `APP_ENV=development`).
Without HTTPS in front, a browser on plain HTTP will not send them back.

Operators and NetBox's webhook (`POST /api/netbox-webhook`) are the
callers that need this HTTPS port. Worker hosts reach the API through
the hub unix socket, not this proxy — see [Worker nodes](workers.md).

Do **not** put worker `/hub` on this proxy. That is a separate WSS
listener on `worker.listen`.

## What the proxy must do

- Terminate TLS and proxy HTTP to `web.bind`.
- Preserve the `Host` header (Caddy does this by default). The admin log
  window opens a WebSocket at `/api/logs/ws`; the backend rejects the
  upgrade if `Origin` and `Host` disagree.
- Pass WebSocket upgrades for `/api/logs/ws`.
- Not buffer `POST /api/worker/run` (NDJSON). Caddy's default already
  flushes streaming responses.

Do not publish `web.bind` on the network. Firewall inbound HTTP/HTTPS
for operators and NetBox; leave `:8091` (or whatever you set) on
loopback.

After the proxy is up, set **Admin → Settings → Factum → public URL** to
the HTTPS origin (e.g. `https://factum.example.com`). Password-reset
mail uses that value. Behind a proxy, do not leave it blank — the
fallback origin is derived from the request the Go process sees, which
is HTTP to loopback.

## Caddy

Certificates are assumed to be on disk already. This does not use
Caddy's automatic ACME.

1. Install Caddy if it is not already there.

2. Put a Caddyfile at `/etc/caddy/Caddyfile` (Debian/Ubuntu package
   default). A copy lives at `examples/Caddyfile`:

```
factum.example.com {
	tls /etc/ssl/certs/factum.example.com.crt /etc/ssl/private/factum.example.com.key
	reverse_proxy 127.0.0.1:8091
}
```

Replace the hostname, certificate paths, and backend port to match this
host. The certificate file must include any intermediate certs. A Let's
Encrypt *fullchain* is that bundle:

```
tls /etc/letsencrypt/live/factum.example.com/fullchain.pem /etc/letsencrypt/live/factum.example.com/privkey.pem
```

The `caddy` user must be able to read the private key. Caddy listens on
`:443` and redirects HTTP `:80` to HTTPS.

3. Validate and reload:

```sh
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

4. Confirm `factum2-web` is on loopback (`ss -ltnp | grep 8091`, or
   whatever `web.bind` is) and that `https://factum.example.com` serves
   the GUI.

If you change `web.bind`, update the `reverse_proxy` target to match.

## Traefik and Apache

Same shape: TLS on `:443`, proxy to `127.0.0.1:8091`, keep `Host`, allow
WebSocket on `/api/logs/ws`. Apache needs `mod_proxy`, `mod_proxy_http`,
and `mod_proxy_wstunnel`. Traefik is a router to that loopback service
with file certificates (or its own resolver).
