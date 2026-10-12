from __future__ import annotations

import io
import os
import pathlib
import shlex
import sys
import tarfile
import tempfile

import paramiko


ROOT = pathlib.Path(__file__).resolve().parent.parent


def load_env(path: pathlib.Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        key, sep, value = line.partition("=")
        if sep:
            values[key.strip()] = value.strip()
    return values


def add_source(archive: tarfile.TarFile) -> None:
    excluded_dirs = {".git", "node_modules", "dist", "coverage"}
    for base, dirs, files in os.walk(ROOT):
        dirs[:] = [name for name in dirs if name not in excluded_dirs]
        base_path = pathlib.Path(base)
        for name in files:
            source = base_path / name
            relative = source.relative_to(ROOT)
            if relative == pathlib.Path(".env"):
                continue
            archive.add(source, arcname=relative.as_posix(), recursive=False)


def run(client: paramiko.SSHClient, command: str) -> None:
    _, stdout, stderr = client.exec_command(command, get_pty=False)
    for line in iter(stdout.readline, ""):
        print(line, end="")
    errors = stderr.read().decode("utf-8", "replace")
    status = stdout.channel.recv_exit_status()
    if errors:
        print(errors, file=sys.stderr, end="")
    if status:
        raise RuntimeError(f"Remote command failed with exit status {status}")


def main() -> None:
    cfg = load_env(ROOT / ".env")
    required = {
        "VPS_IP", "VPS_USER", "VPS_PASSWORD", "VPS_SSH_PORT", "APP_DOMAIN",
        "SDK_PUBLIC_HOST", "PANEL_PUBLIC_HOST", "API_PUBLIC_HOST", "OIDC_PUBLIC_HOST",
        "ACME_EMAIL", "INITIAL_ADMIN_EMAIL", "INITIAL_ADMIN_USERNAME", "INITIAL_ADMIN_PASSWORD",
        "APP_ENV", "DEPLOYMENT_MODE", "POSTGRES_DB", "POSTGRES_MIGRATION_USER",
        "OIDC_REALM", "OIDC_AUDIENCE", "OIDC_CLIENT_ID", "STORAGE_PROJECT_QUOTA_BYTES",
    }
    missing = sorted(name for name in required if not cfg.get(name))
    if missing:
        raise RuntimeError("Missing .env values: " + ", ".join(missing))
    if cfg["VPS_USER"] != "root" or cfg["APP_ENV"] != "production":
        raise RuntimeError("VPS_USER must be root and APP_ENV must be production")

    allowed = [
        "VPS_SSH_PORT", "VPS_TRUSTED_IP_CIDR", "APP_DOMAIN", "SDK_PUBLIC_HOST",
        "PANEL_PUBLIC_HOST", "API_PUBLIC_HOST", "OIDC_PUBLIC_HOST", "ACME_EMAIL",
        "INITIAL_ADMIN_EMAIL", "INITIAL_ADMIN_USERNAME", "INITIAL_ADMIN_PASSWORD",
        "APP_ENV", "DEPLOYMENT_MODE", "POSTGRES_DB", "POSTGRES_MIGRATION_USER",
        "OIDC_REALM", "OIDC_AUDIENCE", "OIDC_CLIENT_ID", "STORAGE_PROJECT_QUOTA_BYTES",
    ]
    deploy_env = "".join(f"{name}={shlex.quote(cfg.get(name, ''))}\n" for name in allowed).encode()

    deployment_id = os.urandom(8).hex()
    with tempfile.TemporaryDirectory(prefix="backlog-killer-deploy-") as temp:
        bundle = pathlib.Path(temp) / "source.tar.gz"
        with tarfile.open(bundle, "w:gz") as archive:
            add_source(archive)

        client = paramiko.SSHClient()
        client.load_system_host_keys()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        print("Connecting to VPS with credentials from .env")
        client.connect(
            hostname=cfg["VPS_IP"],
            port=int(cfg["VPS_SSH_PORT"]),
            username=cfg["VPS_USER"],
            password=cfg["VPS_PASSWORD"],
            look_for_keys=False,
            allow_agent=False,
            timeout=20,
            banner_timeout=60,
            auth_timeout=20,
        )
        try:
            sftp = client.open_sftp()
            try:
                remote_bundle = f"/root/source-{deployment_id}.tar.gz"
                remote_config = f"/root/deploy-{deployment_id}.env"
                sftp.put(str(bundle), remote_bundle)
                with sftp.file(remote_config, "wb") as target:
                    target.write(deploy_env)
                sftp.chmod(remote_config, 0o600)
            finally:
                sftp.close()
            run(client, f"rm -rf /root/backlog-killer-source && mkdir -p /root/backlog-killer-source && tar -xzf {remote_bundle} -C /root/backlog-killer-source && rm -f {remote_bundle} && bash /root/backlog-killer-source/deploy/native/provision.sh {remote_config}")
        finally:
            client.close()


if __name__ == "__main__":
    main()
