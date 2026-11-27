#!/usr/bin/env bash
# Generates the TLS material for the LDAP login tests into DIR: a CA, a server
# certificate for the LDAP server signed by it, and a second CA that did not
# sign anything. Run it when the test environment starts; nothing it writes
# is committed.
#
# usage: gen-certs.sh DIR [HOSTNAME...]
# The server certificate is valid for localhost, 127.0.0.1 and every HOSTNAME.
# Extensions are set explicitly, so OpenSSL and LibreSSL produce the same
# certificates.
set -euo pipefail

dir=${1:?usage: gen-certs.sh DIR [HOSTNAME...]}
shift
mkdir -p "$dir"
cd "$dir"

san="DNS:localhost,IP:127.0.0.1"
for host in "$@"; do
  san="$san,DNS:$host"
done

cat > openssl.cnf <<EOF
[req]
distinguished_name = dn
[dn]
[ca]
basicConstraints = critical,CA:true
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
[server]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = $san
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid
EOF

for ca in ca wrong-ca; do
  cn="pulumi-dex LDAP test CA"
  [ "$ca" = wrong-ca ] && cn="pulumi-dex wrong CA"
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=$cn" -config openssl.cnf -extensions ca \
    -keyout "$ca.key" -out "$ca.crt" 2>/dev/null
done
openssl req -new -newkey rsa:2048 -nodes -subj "/CN=localhost" -config openssl.cnf -keyout ldap.key -out ldap.csr 2>/dev/null
openssl x509 -req -in ldap.csr -CA ca.crt -CAkey ca.key -CAcreateserial -days 2 \
  -extfile openssl.cnf -extensions server -out ldap.crt 2>/dev/null
openssl dhparam -dsaparam -out dhparam.pem 2048 2>/dev/null
rm -f ldap.csr ca.srl ca.key wrong-ca.key openssl.cnf
# The LDAP server process may run as another user.
chmod 644 ldap.key
