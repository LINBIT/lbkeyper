# lbkeyper

`lbkeyper` consists of a server and a client part. The server is started with a `toml` configuration that
allows specifying users with their keys, servers and groups.

The server exposes a http(s) API hat can be used to query the keys for a user on a particular host. Usually
the daemon is not queried directly but from `sshd` via a shell script. On can get the shell script via the
`/auth.sh` endpoint (e.g., `curl https://lbkeyper.your.company/auth.sh`). The end of the generated shell
script contains configuration information for the local `sshd` daemon.

# Containers

```
docker run -it --rm \
  -p 80:80 \
  -v $PWD/config.toml:/config.toml:ro \
  lbkeyper -url http://lbkeyper.your.domain:80 -config /config.toml
```
