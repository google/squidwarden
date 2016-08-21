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
$ sudo apt install squid3
$ ./go get github.com/google/squidwarden
$ sudo mv /etc/squid3/squid.conf{,.dist}
$ cat <<EOF > /etc/squid3/squid.conf
# TODO: Not all of these settings may be needed.
http_port 3128
via off
forwarded_for delete
error_directory /etc/squid3/myerrors

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
$ sudo cp bin/helper /usr/local/bin/proxyacl
$ sudo sqlite3 /var/spool/squid3/proxyacl.sqlite < src/github.com/google/squidwarden/sqlite.schema
$ sudo systemctl restart squid3
$ sudo -u proxy ./bin/ui \
    -templates=src/github.com/google/squidwarden/cmd/ui/templates \
    -static=src/github.com/google/squidwarden/cmd/ui/static \
    -addr=:8081 \
    -squidlog=/var/log/squid3/proxyacl.blocklog \
    -db=/var/spool/squid3/proxyacl.sqlite
```

Then point browser to http://localhost:8080/ and get started.
