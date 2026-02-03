#!/bin/sh
KEYPER_SERVER='{{ .KeyperServer }}'
HOST="$(hostname)"
USER="$(id -un)"

die() {
	2>&1 echo "$@"
	exit 1
}

KEYS_OUTPUT="$(curl -q -s -f -m 5 "${KEYPER_SERVER}/api/v1/keys/${HOST}/${USER}")"
test -n "${KEYS_OUTPUT}" || die "Refusing to install: ${KEYPER_SERVER} has no configuration for ${USER}@${HOST}"

DOWNLOAD_FILE="$(mktemp -p /etc/ssh auth.sh.XXXX)"
trap "rm -f -- '$DOWNLOAD_FILE'" EXIT

curl -q -s -f -m 5 "${KEYPER_SERVER}/auth.sh" -o "${DOWNLOAD_FILE}" || die "Failed to download auth.sh script"
mv "${DOWNLOAD_FILE}" /etc/ssh/auth.sh
chown root:root /etc/ssh/auth.sh
chmod 0700 /etc/ssh/auth.sh

if command -v restorecon >/dev/null 2>&1; then
	restorecon /etc/ssh/auth.sh
fi

if [ -d /etc/ssh/sshd_config.d ]; then
	cat <<EOF > /etc/ssh/sshd_config.d/keyper.conf || die "Failed to write sshd configuration to /etc/ssh/sshd_config.d/keyper.conf"
AuthorizedKeysCommand /etc/ssh/auth.sh
AuthorizedKeysCommandUser root
EOF
else
	sed -i.lbkeyper-setup -E \
		-e 's%^(#\s*)?AuthorizedKeysCommand\b.*%AuthorizedKeysCommand /etc/ssh/auth.sh%' \
		-e 's%^(#\s*)?AuthorizedKeysCommandUser\b.*%AuthorizedKeysCommandUser root%' \
		/etc/ssh/sshd_config \
		|| die "Failed to update sshd configuration /etc/ssh/sshd_config"
fi

if ! sshd -qt ; then
	if [ -e /etc/ssh/sshd_config.lbkeyper-setup ]; then
		mv /etc/ssh/sshd_config.lbkeyper-setup /etc/ssh/sshd_config
	else
		rm -rf /etc/ssh/sshd_config.d/lbkeyper.conf
	fi
	die "Created invalid sshd configuration, rolling back changes"
fi

if [ -e /sys/fs/selinux ]; then
	mkdir -p /usr/local/src/lbkeyper-selinux
	cat <<EOF > /usr/local/src/lbkeyper-selinux/lbkeyper.te
module lbkeyper 1.0;

require {
	type var_run_t;
	type http_port_t;
	type sshd_t;
	type hostname_exec_t;
	class file { execute execute_no_trans map open read };
	class dir create;
	class tcp_socket name_connect;
}

#============= sshd_t ==============
allow sshd_t hostname_exec_t:file { execute execute_no_trans open read };
allow sshd_t hostname_exec_t:file map;
allow sshd_t http_port_t:tcp_socket name_connect;
allow sshd_t var_run_t:dir create;
EOF

	checkmodule -M -m -o /usr/local/src/lbkeyper-selinux/lbkeyper.mod /usr/local/src/lbkeyper-selinux/lbkeyper.te || die "Failed to check SELinux module"
	semodule_package -o /usr/local/src/lbkeyper-selinux/lbkeyper.pp -m /usr/local/src/lbkeyper-selinux/lbkeyper.mod || die "Failed to package SELinux module"
	semodule -i /usr/local/src/lbkeyper-selinux/lbkeyper.pp || die "Failed to install SELinux module"
fi

systemctl reload sshd

echo "Successfully installed! Try logging in and check /run/sshd/lbkeyper/${USER} exists"
