# lbkeyper

`lbkeyper` allows you to centrally manage `AuthorizedKeys` for your users and servers. User keys can be
specified in a configuration or fetched from Github/Gitlab. The client part implements a key cache, so that
you are not locked out if the daemon is currently not accessible.

`lbkeyper` consists of a server and a client part. The server is started with a `toml` configuration that
allows specifying users with their keys, user groups, servers and server groups.

The server exposes a http(s) API hat can be used to query the keys for a user on a particular host. Usually
the daemon is not queried directly but from `sshd` via a shell script. One can get this shell script via the
`/auth.sh` endpoint. The end of the generated shell script contains commented configuration information for
the local `sshd` daemon.

# Example configuration
Here we assume a small company (acme.com) with 3 users and a handful of servers including www servers and a
package build server. The admin would probably allocate a lbkeyper.acme.com server (or use an existing one) and
write a configuration file similar to the following:

```
[users]
[users.alice]
keys = [
  "https://gitlab.acme.com/alice.keys",
  "ssh-ed25519 AAAAC3NzaC... alice@laptop"
]

[users.bob]
keys = [ "https://github.com/bob.keys" ]

[users.charlie]
keys = [ "https://github.com/charlie.keys" ]


[usergroups.admins]
members = [ "alice", "charlie" ]

[usergroups.pkgmaintainers]
members = [ "alice", "bob" ]

[servers.builder]
mapusers = true  # this allows non specified users to log in as well (e.g., alice@builder)
[servers.builder.users]
build = [ "@pkgmaintainers", "charlie" ]

[servergroups.www]
members = [ "www", "www2", "www3" ]
[servergroups.www.users]
root = [ "@admins" ]
uploader = [ "@pkgmaintainers" ]
```

Let's discuss the example top down. First we have the `[users]` section that defines individual users and
their public ssh keys. Here we see that Alice has one typical ssh public key starting with "ssh-ed25519", and
other keys that are automatically fetched via https. Github and Gitlab for example allow retrieving keys like
that. In genral every http(s) server that returns public keys on http-GET should work.

To ease configuration, users can be grouped. In our example we see that Alice and Charlie are in the user
group "admins".

The main sections are `[servers]` and `[servergroups]`. This basically defines a mapping between
ssh usernames and users defined in the config. As you can see, user groups are referenced via `@groupname`. In
the example above the server "builder" can be accessed by the ssh user "build", and all users in the
"pkgmaintainers" user group and "charlie" are allowed. Sometimes there are servers where all your users have
accounts and where all of these users should be able to log in. This would require mappings like `user1 =
user1`. To avoid that, one can set `mapusers = true`, and all users defined in the `[users]` section are
mapped automatically.

Sometimes there are servers that need the same permissions, or they are part of a larger cluster. One can group
these via server groups as shown for the www servers. Here we define a `servergroup` named "www", and then we
define all of its members. An entry of a `servergroup` can specify all the keys an ordinary `server`
section can (i.e., a list of `users`, and `mapusers`). In our example Alice, as part of the user group
"admins" would be allowed to access the server "www2" as user "root".

After writing the config and starting the daemon, we assume `https://lbkeyper.acme.com`, a first test would be
`curl https://lbkeyper.acme.com/api/v1/hello`. This should be successful and return the commit hash of the
running daemon.

The next step would be a sample query like `curl -L https://lbkeyper.acme.com/api/v1/keys/builder/charlie`, which
should return Charlie's public keys.

Finally one would integrate it on a host like "builder":

```
root@builder$ curl -fsSL https://lbkeyper.acme.com/setup.sh | bash -s
```

Alternatively, if automatic configuration fails or is undesired:

```
root@builder$ cd /etc/ssh
root@builder$ curl https://lbkeyper.acme.com/auth.sh > auth.sh
root@builder$ cat auth.sh to see the commented configuration options
root@builder$ chown root:root auth.sh
root@builder$ chmod 700 auth.sh
root@builder$ ./auth.sh root # final test to see allowed keys for root
root@builder$ vim sshd_config # set at least AuthorizedKeyCommand and AuthorizedKeyCommandUser
root@builder$ systemctl restart sshd
```

# Containers

```
docker run -it --rm \
  -p 80:80 \
  -v $PWD/config.toml:/config.toml:ro \
  lbkeyper -url http://lbkeyper.your.domain -config /config.toml
```
