#!/bin/sh
set -eu

: "${SSH_TEST_USER:=tester}"
: "${SSH_TEST_PASSWORD:=pglight-test}"

ssh-keygen -A
if ! id "$SSH_TEST_USER" >/dev/null 2>&1; then
  adduser -D "$SSH_TEST_USER"
fi
printf '%s:%s\n' "$SSH_TEST_USER" "$SSH_TEST_PASSWORD" | chpasswd

cat > /etc/ssh/sshd_config <<EOF
Port 22
HostKey /etc/ssh/ssh_host_ed25519_key
HostKey /etc/ssh/ssh_host_rsa_key
UsePAM no
PubkeyAuthentication yes
PasswordAuthentication yes
PermitRootLogin no
AllowUsers $SSH_TEST_USER
AuthorizedKeysFile .ssh/authorized_keys
EOF

exec /usr/sbin/sshd -D -e
