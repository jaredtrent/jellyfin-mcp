# Remote Access Setup

Reach Jellyfin from outside the home network through a reverse proxy, a VPN, or a forwarded port, and tell Jellyfin which proxy to trust.

Jellyfin listens on port 8096 for HTTP and on 8920 for HTTPS. A reverse proxy in front of it terminates TLS and forwards requests, a VPN keeps the server off the public internet, and port forwarding exposes it directly. Choose the proxy for a server that several households use, and the VPN for one that only you and people you trust reach.

## Option 1: Reverse Proxy

A reverse proxy terminates HTTPS with a certificate from Let's Encrypt and forwards plain HTTP to Jellyfin on the same host. Jellyfin reads the client's real address from the `X-Forwarded-For`, `X-Forwarded-Proto`, and `X-Forwarded-Host` headers, but only from a proxy it trusts. Add the proxy's IP address under Known Proxies in Dashboard > Networking. Without that entry, Jellyfin sees every client as the proxy, so per-user remote access rules and local network detection stop working.

### Caddy

Caddy provisions and renews the certificate itself:

~~~
jellyfin.example.com {
    reverse_proxy 127.0.0.1:8096
}
~~~

### Nginx

Nginx 1.25 and newer takes `http2 on;` as a separate directive. The `/socket` block passes the WebSocket upgrade that SyncPlay and session updates need, and `proxy_buffering off` keeps a long stream from exhausting the proxy. The port 80 block redirects to HTTPS and lets Certbot renew the certificate.

~~~nginx
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name jellyfin.example.com;
    client_max_body_size 20M;
    ssl_protocols TLSv1.3 TLSv1.2;
    ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
    ssl_trusted_certificate /etc/letsencrypt/live/example.com/chain.pem;
    set $jellyfin 127.0.0.1;
    add_header X-Content-Type-Options "nosniff";

    location / {
        proxy_pass http://$jellyfin:8096;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Protocol $scheme;
        proxy_set_header X-Forwarded-Host $http_host;
        proxy_buffering off;
    }

    location /socket {
        proxy_pass http://$jellyfin:8096;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Protocol $scheme;
        proxy_set_header X-Forwarded-Host $http_host;
    }
}

server {
    listen 80;
    listen [::]:80;
    server_name jellyfin.example.com;
    return 301 https://$host$request_uri;
}
~~~

The official configuration also sets `Permissions-Policy` and `Content-Security-Policy` headers; copy them from the Nginx page in the sources.

### Apache

Turn on the modules first:

~~~
sudo a2enmod proxy proxy_http ssl proxy_wstunnel remoteip http2 headers
~~~

The two rewrite rules send WebSocket upgrades to `/socket` and everything else to Jellyfin over HTTP. The `X-Forwarded-Proto` header tells Jellyfin that the client used HTTPS.

~~~apache
<VirtualHost *:443>
    ServerName jellyfin.example.com
    ProxyPreserveHost On
    ProxyPass "/.well-known/" "!"
    RequestHeader set X-Forwarded-Proto "https"
    RequestHeader set X-Forwarded-Port "443"

    RewriteEngine On
    RewriteCond %{HTTP:Upgrade} =websocket
    RewriteRule /(.*) ws://127.0.0.1:8096/socket/$1 [P,L]
    RewriteCond %{HTTP:Upgrade} !=websocket
    RewriteRule /(.*) http://127.0.0.1:8096/$1 [P,L]

    SSLEngine on
    SSLCertificateFile /etc/letsencrypt/live/jellyfin.example.com/fullchain.pem
    SSLCertificateKeyFile /etc/letsencrypt/live/jellyfin.example.com/privkey.pem
    Protocols h2 http/1.1
    SSLProtocol all -SSLv2 -SSLv3 -TLSv1 -TLSv1.1
</VirtualHost>
~~~

### Base URL

Set a base URL only when Jellyfin shares a domain with other services on different paths, such as `https://example.com/jellyfin`. Set it to `/jellyfin` in Dashboard > Networking, restart the server, and then route that path in the proxy. A dedicated subdomain needs no base URL.

## Option 2: VPN

A VPN such as Tailscale or WireGuard connects each device to the home network, so Jellyfin never faces the public internet and needs no certificate or forwarded port. Tailscale sets itself up from an account; WireGuard needs a key exchange for each device. Clients then use the server's VPN address.

## Option 3: Port Forwarding

Forwarding port 8096 or 8920 on the router exposes Jellyfin directly to the internet. Every login form becomes reachable by anyone, and Jellyfin has no rate limiting beyond locking an account after repeated failed logins. Jellyfin can serve HTTPS itself on port 8920 with a certificate you supply, but the project has announced that built-in TLS will go away in a future version, so a reverse proxy is the durable choice.

## Sources

- https://jellyfin.org/docs/general/post-install/networking/
- https://jellyfin.org/docs/general/post-install/networking/reverse-proxy/
- https://jellyfin.org/docs/general/post-install/networking/reverse-proxy/nginx/
- https://jellyfin.org/docs/general/post-install/networking/reverse-proxy/apache/
- https://jellyfin.org/docs/general/post-install/networking/reverse-proxy/caddy/
