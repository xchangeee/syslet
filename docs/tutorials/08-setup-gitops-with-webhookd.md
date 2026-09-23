# Set up GitOps with webhookd

So far you've applied every change from your workstation with `cue cmd apply`.
With GitOps, changes pushed to git are applied automatically: you push to the repository, the git host calls a webhook on the server, and the server pulls the repository, evaluates the CUE config and runs syslet.

In this tutorial, you'll set that up for `web01` with [webhookd](https://github.com/ncarlier/webhookd), a small server that runs a shell script for each HTTP request.
Because the server evaluates the CUE config itself, it needs CUE installed too.

## Prerequisites

- The `infra` repository from [Add nginx basic auth](07-add-nginx-basic-auth.md), pushed to a git host that supports deploy keys and webhooks, such as Codeberg, GitHub or GitLab.
  This tutorial calls it `git@git.example.com:you/infra.git`, with the branch `main`.
- `git` on the server.
- The git host can reach `web01.example.com` on port `38080`.

## 1. Install CUE and webhookd

On the server, install the CUE version the repository uses and the latest webhookd:

```sh
curl -sSfL https://github.com/cue-lang/cue/releases/download/v0.16.1/cue_v0.16.1_linux_amd64.tar.gz \
  | sudo tar -xz -C /usr/local/bin cue
curl -sSfL https://github.com/ncarlier/webhookd/releases/latest/download/webhookd-linux-amd64.tgz \
  | sudo tar -xz -C /usr/local/bin webhookd
```

Replace `amd64` with `arm64` as needed.

## 2. Give the server read access

The server needs its own key to clone the repository:

```sh
sudo mkdir -p /etc/webhookd/scripts
sudo ssh-keygen -t ed25519 -f /etc/webhookd/deploykey -N "" -C "web01-deploy"
sudo cat /etc/webhookd/deploykey.pub
```

Add the printed public key to the repository as a read-only deploy key, then test it:

```sh
sudo ssh -i /etc/webhookd/deploykey -o StrictHostKeyChecking=accept-new -T git@git.example.com
```

The git host greets you and closes the connection.

## 3. Add the deploy script

webhookd runs `/etc/webhookd/scripts/<name>.sh` for requests to `/<name>`.
Create the script for `/deploy`, with your repository URL:

```bash title="/etc/webhookd/scripts/deploy.sh"
--8<-- "08-setup-gitops-with-webhookd/deploy.sh"
```

```sh
sudo chmod 700 /etc/webhookd/scripts/deploy.sh
```

The script clones the repository on the first run and resets it to `origin/main` on every run after.
It then does on the server what `cue cmd apply` did on your workstation, without asking for confirmation.
`cue export` downloads the syslet schema module on its first run and caches it in `CUE_CACHE_DIR`.

## 4. Run webhookd

Anyone who can call `/deploy` can trigger a deployment, so protect it with a password.
Create the password file, with `htpasswd` from `httpd-tools` (`apache2-utils` on Debian):

```sh
sudo htpasswd -B -c /etc/webhookd/users.htpasswd deploy
```

webhookd starts without authentication if it can't read this file, so check the path.

Configure webhookd:

```sh title="/etc/webhookd/webhookd.env"
--8<-- "08-setup-gitops-with-webhookd/webhookd.env"
```

Run it as a systemd service:

```ini title="/etc/systemd/system/webhookd.service"
--8<-- "08-setup-gitops-with-webhookd/webhookd.service"
```

webhookd runs as root, because syslet needs root.
`StateDirectory` and `CacheDirectory` create `/var/lib/webhookd` and `/var/cache/webhookd` for the script.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now webhookd.service
```

## 5. Trigger a deployment by hand

From your workstation, call the hook with the password you set:

```sh
curl -u deploy -X POST http://web01.example.com:38080/deploy
```

webhookd streams the script's output.
You see the clone, then syslet's summary, which ends with:

```text
No changes detected. All units are up to date.
```

The server already runs what's in `main`, so there's nothing to do.
A missing password returns `401 Unauthorized`.

## 6. Wire up the webhook

In the repository's settings on your git host, add a webhook for push events with this URL:

```text
http://deploy:<password>@web01.example.com:38080/deploy
```

Some git hosts don't accept credentials in the URL; they have a separate field for an `Authorization` header instead, which takes `Basic <base64>`, where `<base64>` is `deploy:<password>` encoded with `base64`.

The password travels in plain text over HTTP.
Once the server has a DNS name reachable from the git host, set `WHD_TLS_ENABLED=true` and `WHD_TLS_DOMAIN` in `webhookd.env` so webhookd serves HTTPS with a Let's Encrypt certificate.

## 7. Deploy with a push

Update nginx in `web01.cue`:

```cue title="web01.cue" hl_lines="7"
--8<-- "08-setup-gitops-with-webhookd/web01.cue"
```

Preview the change as before, then push it instead of applying it:

```sh
cue cmd plan
git add -A && git commit -m "update nginx" && git push
```

The git host calls the webhook, and the server pulls the new image and restarts `site.container`.
Your git host lists each webhook delivery with webhookd's response, which holds the script's output.
webhookd streams that output, so it answers `200` even when the script fails; read the output to see whether the deployment worked.
Check the result on the server:

```sh
ssh web01.example.com podman ps
```

From now on, a push to `main` is a deployment.
Keep using `cue cmd plan` to preview changes, but leave `cue cmd apply` to the server.
