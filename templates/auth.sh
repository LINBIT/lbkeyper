#!/bin/sh
KEYPER_SERVER='{{ .KeyperServer }}'
USER="$1"
CACHE=/run/sshd/lbkeyper
CACHEFILE="${CACHE}/${USER}"
HOST="$(hostname)"

mkdir -p "$CACHE"  # some minimal cache if KEYPER_SERVER is not accessible
TMPFILE="$(mktemp -p "$CACHE")" || { cat "${CACHEFILE}"; exit; }
trap "rm -f -- '$TMPFILE'" EXIT
if curl -q -s -f -m 5 "${KEYPER_SERVER}/api/v1/keys/${HOST}/${USER}" > "$TMPFILE"; then
   if [ -s "${TMPFILE}" ]; then
      mv "${TMPFILE}" "${CACHEFILE}"
   else
      rm -f "${CACHEFILE}"  # user got removed
   fi
fi
test -f "${CACHEFILE}" && cat "${CACHEFILE}"

### CONFIGURATION
# Run:
#
#  curl -fsSL {{ .KeyperServer }}/setup.sh | bash -s
#
# Alternatively:
#
# - save this file to /etc/ssh/auth.sh
# - chown root:root /etc/ssh/auth.sh
# - chmod 700 /etc/ssh/auth.sh
# - sshd_config(.d):
# AuthorizedKeysCommand /etc/ssh/auth.sh
# AuthorizedKeysCommandUser root  # or some dedicated user, however you feel like, but take care of the cache and other perms
# # AuthorizedKeysFile none  # optional, but most likely a good idea...
# # PermitRootLogin prohibit-password  # optional, but most likely a good idea...
# # PasswordAuthentication no  # optional, but most likely a good idea...
#
# On systems with SELinux, you might need this policy module:
#
# module lbkeyper 1.0;
#
# require {
# 	type var_run_t;
# 	type http_port_t;
# 	type sshd_t;
# 	type hostname_exec_t;
# 	class file { execute execute_no_trans map open read };
# 	class dir create;
# 	class tcp_socket name_connect;
# }
#
# #============= sshd_t ==============
# allow sshd_t hostname_exec_t:file { execute execute_no_trans open read };
#
# #!!!! This avc can be allowed using the boolean 'domain_can_mmap_files'
# allow sshd_t hostname_exec_t:file map;
#
# #!!!! This avc can be allowed using one of the these booleans:
# #     authlogin_yubikey, nis_enabled
# allow sshd_t http_port_t:tcp_socket name_connect;
# allow sshd_t var_run_t:dir create;
