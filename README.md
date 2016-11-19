# Squidwarden

Frontend to managaging ACLs for the Squid proxy.

Copyright 2016 Google Inc. All Rights Reserved.
Apache 2.0 license.

This is NOT a Google product.

Contact: thomas@habets.se / habets@google.com
https://github.com/google/squidwarden/

## Install

TODO: This procedure is untested.

```
$ sudo apt install squid3 sqlite3
$ go get github.com/google/squidwarden/...
$ go generate github.com/google/squidwarden/...
$ sudo mv /etc/squid3/squid.conf{,.dist}
$ sudo dd of=/etc/squid3/squid.conf <<EOF
# TODO: Not all of these settings may be needed.
http_port 3128
via off
forwarded_for delete
# error_directory /etc/squid3/myerrors

acl success_hier hier_code HIER_DIRECT
acl failure_hier hier_code HIER_NONE
access_log daemon:/var/log/squid3/access.log squid failure_hier

external_acl_type ext ttl=10 concurrency=2 %PROTO %SRC %METHOD %URI /usr/local/bin/proxyacl -db=/var/spool/squid3/proxyacl.sqlite -log=/var/log/squid3/proxyacl.log -block_log=/var/log/squid3/proxyacl.blocklog
acl ext_acl external ext
http_access allow ext_acl

visible_hostname my.proxy.hostname.here.example.com

# Default suffix.
http_access deny all
EOF
$ sudo mv bin/helper /usr/local/bin/proxyacl
$ sudo -u proxy sqlite3 /var/spool/squid3/proxyacl.sqlite < src/github.com/google/squidwarden/sqlite.schema
$ sudo systemctl restart squid3
$ sudo mv bin/ui /usr/local/bin/squidwarden
$ sudo -u proxy /usr/local/bin/squidwarden \
    -addr=:8081 \
    -squidlog=/var/log/squid3/proxyacl.blocklog \
    -https_only=false \
    -db=/var/spool/squid3/proxyacl.sqlite
```

Then point browser to [the UI](http://localhost:8081/) and get started.

## Run UI via nginx

It can be a good idea to run through a real web server such as nginx,
so that you don't have to remember which port it runs on. It also makes
it easier to set up TLS.

```
$ sudo apt-get install nginx
$ sudo dd of=/etc/nginx/conf.d/squidwarden.conf <<EOF
map \$http_upgrade \$connection_upgrade {
  default upgrade;
  '' close;
}
server {
    listen 80;
    listen [::]:80;
    server_name squidwarden.example.com;
    location / {
        # Add any auth stuff here.
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "\$connection_upgrade";
    }
}
EOF
$ sudo systemctl restart nginx.service
$ sudo -u proxy /usr/local/bin/squidwarden \
    -templates=src/github.com/google/squidwarden/cmd/ui/templates \
    -static=src/github.com/google/squidwarden/cmd/ui/static \
    -addr=127.0.0.1:8081 \
    -https_only=false \
    -squidlog=/var/log/squid3/proxyacl.blocklog \
    -db=/var/spool/squid3/proxyacl.sqlite
```

### Set up auth

```
$ echo -n 'admin:' | sudo tee of=/etc/nginx/htpasswd
$ openssl passwd -apr1 | sudo tee -a /etc/nginx/htpasswd
Password:
Verifying - Password:
```

Then add this to `/etc/nginx/conf.d/squidwarden.conf` inside the
`location /` section.

```
        auth_basic "Restricted Content";
        auth_basic_user_file /etc/nginx/htpasswd;
```

## Run UI with fastcgi nginx

FastCGI is nice, but doesn't support websockets. When `-fcgi` is
supplied, squidwarden will therefore not use websockets.

```
$ sudo apt-get install nginx
$ sudo dd of=/etc/nginx/conf.d/squidwarden.conf <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name squidwarden.example.com;
    location / {
      include fastcgi_params;
      fastcgi_pass unix:/var/spool/squid3/squidwarden.sock;
    }
}
EOF
$ sudo systemctl restart nginx.service
$ sudo -u proxy /usr/local/bin/squidwarden \
    -addr=127.0.0.1:8081 \
    -fcgi=/var/spool/squid3/squidwarden.sock \
    -https_only=false \
    -squidlog=/var/log/squid3/proxyacl.blocklog \
    -db=/var/spool/squid3/proxyacl.sqlite
```
