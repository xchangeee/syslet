# Add nginx basic auth

In this tutorial, you'll protect an `/admin/` path on the `site` container with HTTP basic auth.
The password file is an encrypted secret in the repository, which syslet syncs to podman and podman mounts into the container.

## Prerequisites

- The `infra` repository with sops set up, from [Set up SOPS encryption](06-setup-sops-encryption.md).

## 1. Create a secret

Create `creds-site.enc.yaml` with the password file as its only key:

```yaml title="creds-site.enc.yaml"
--8<-- "07-add-nginx-basic-auth/step-1/creds-site.enc.yaml"
```

The file must be a flat mapping of string values, and key names may only contain `a-z`, `0-9` and `-`.
Each key becomes one podman secret named `<spec name>-<key>`, here `site-htpasswd`.

Encrypt it in place before you do anything else with it:

```sh
sops -e -i creds-site.enc.yaml
```

The key stays readable and the value is replaced by ciphertext, followed by the `sops` metadata with one entry per recipient:

```yaml title="creds-site.enc.yaml"
--8<-- "07-add-nginx-basic-auth/step-1-encrypted/creds-site.enc.yaml"
```

From now on, `sops edit creds-site.enc.yaml` opens the file decrypted in your editor and re-encrypts it on save.

The CUE config from the previous tutorial already turns this file into the secret spec `site`.

## 2. Use the secret in a container

Reference the secret from the container's `Secret` option in `web01.cue`, and protect `/admin/` in the nginx config:

```cue title="web01.cue" hl_lines="9-11 21-24"
--8<-- "07-add-nginx-basic-auth/web01.cue"
```

Each entry maps a podman secret name to the options of quadlet's `Secret=`, and syslet renders it as `Secret=site-htpasswd,uid=101,gid=101,mode=0400`.
The reference is a plain config string: the container spec only names the podman secret and never contains its value.
podman mounts the secret as a file at `/run/secrets/site-htpasswd`, readable only by nginx's worker user.

syslet checks every `Secret=` reference against the keys of the secret specs before decrypting anything, so a typo in the name fails the plan.

Preview it:

```sh
cue cmd plan
```

The plan lists the unit and config changes, the podman secrets syslet will create, and a restart of `site.container`:

```text
Unit file changes:

--- site.container
added:
  [Container] Secret=site-htpasswd,uid=101,gid=101,mode=0400

Config file changes:
...

Secret changes:
  site:
    + htpasswd=admin:{PLAIN}hunter2

...

Summary:
UNIT                                     STATUS     CHANGES
site.container                           updated    unit updated, config updated, secret updated, restarted (desired: running)
```

The plan prints new secret values in plain text, so keep its output out of shared logs such as CI.

Apply and commit:

```sh
cue cmd apply
git add -A && git commit -m "add site admin auth"
```

Check that `/admin/` now asks for credentials:

```sh
curl -i http://web01.example.com:8080/admin/
curl -i -u admin:hunter2 http://web01.example.com:8080/admin/
```

The first request returns `401 Unauthorized`; the second gets past the auth check and returns `404`, since there is no page there yet.

## 3. Rotate the secret

Change the password:

```sh
sops edit creds-site.enc.yaml
cue cmd apply
```

The summary shows `secret updated` for `site.container`: syslet updated the podman secret and restarted the container, so nginx now reads the new password.

The host's secrets now live in the repository alongside its config, and only you and syslet on `web01` can decrypt them.

Continue with [Set up GitOps with webhookd](08-setup-gitops-with-webhookd.md).
