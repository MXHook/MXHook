# exe.dev deployment

The supported exe.dev target uses its native Maildir receiver, embedded SQLite,
and a hardened systemd service. The API remains on VM loopback and is reached
through SSH.

## Provision with pod

Pod's exe.dev adapter requires `EXE_DEV_API_KEY`:

```bash
pod create mxhook --provider exedev --cpu 2 --ram 4 --disk 25 \
  --tag production --note "MXHook production service"
```

The pod adapter provisions lifecycle and SSH coordinates. Application install
uses ordinary `scp`/`ssh` until pod exposes its planned sync/exec commands.

## Install

Build the Linux binary and copy the files:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/mxhook .
scp /tmp/mxhook mxhook.exe.xyz:/tmp/mxhook
scp deploy/exedev/mxhook.service mxhook.exe.xyz:/tmp/mxhook.service
```

On the VM:

```bash
sudo install -o root -g root -m 0755 /tmp/mxhook /usr/local/bin/mxhook
sudo install -d -o exedev -g exedev -m 0700 /var/lib/mxhook
sudo install -d -o root -g exedev -m 0750 /etc/mxhook
sudo install -o root -g root -m 0644 /tmp/mxhook.service /etc/systemd/system/mxhook.service
sudo systemctl daemon-reload
```

Create `/etc/mxhook/mxhook.env` from `mxhook.env.example`, with mode `0640`,
owner `root`, and group `exedev`. Generate independent secrets:

```bash
openssl rand -hex 32
openssl rand -base64 32
```

Start the safe worker/API bootstrap first:

```bash
ssh mxhook.exe.xyz sudo systemctl enable --now mxhook
ssh mxhook.exe.xyz curl -fsS http://127.0.0.1:8080/readyz
```

Create a durable route through the SSH tunnel shown below. Then set
`MXHOOK_RUNTIME_ROLE=all` and
`MXHOOK_MAILDIR_PATH=/home/exedev/Maildir`, restart, confirm readiness, and only
then enable email:

```bash
ssh mxhook.exe.xyz sudo systemctl restart mxhook
ssh exe.dev share receive-email mxhook on --reply-policy=none
```

Email sent to `anything@mxhook.exe.xyz` lands in `/home/exedev/Maildir/new`.
MXHook atomically claims it, stores raw bytes under `/var/lib/mxhook/raw`, and
persists the event and schedules its configured HTTPS delivery.

## Remote API tunnel

```bash
ssh -N -L 18080:127.0.0.1:8080 mxhook.exe.xyz
MXHOOK_API_KEY=... mxhook route add \
  --api-url http://127.0.0.1:18080 \
  --recipient '*@mxhook.exe.xyz' \
  --webhook https://receiver.example.com/email \
  --secret "$(openssl rand -hex 32)"
```

Keep the state encryption key in the recovery store. Back up
`/var/lib/mxhook/mxhook.db` plus WAL/SHM siblings and `/var/lib/mxhook/raw`
together after a graceful stop; see [operations](../../docs/operations.md).
