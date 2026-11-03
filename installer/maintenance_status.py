import configparser
import hashlib
import json
import os
import pathlib
import socket
import ssl
import subprocess
import sys


def command(args):
    return subprocess.run(
        args, check=True, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, timeout=90,
    ).stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def collect():
    base = pathlib.Path("/root/coriolis-docker")
    result = {"healthy": False, "errors": []}

    def check(name, operation):
        try:
            return operation()
        except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError, ssl.SSLError):
            result["errors"].append(name + " failed (details withheld to protect credentials)")
            return None

    release = check("release", lambda: pathlib.Path("/etc/coriolis/coriolis.release").read_text().strip())
    result["release"] = release or ""
    result["package_manager_ready"] = check("package manager readiness (dpkg audit, journals and locks)", check_package_manager) is True
    def usage(path):
        stat = os.statvfs(path)
        used = stat.f_blocks - stat.f_bfree
        return int(used * 100 / (used + stat.f_bavail)) + 1
    result["disk_used_percent"] = max(usage(p) for p in ("/", "/var/lib/docker"))
    result["git_commit"] = check("vendor git revision", lambda: command(["git", "-C", str(base), "rev-parse", "HEAD"]).strip()) or ""
    dirty = check("vendor git changes", lambda: command(["git", "-C", str(base), "status", "--porcelain", "--untracked-files=no"]))
    result["git_clean"] = dirty == ""
    updater = base / "upgrade_coriolis.py"
    result["updater_available"] = updater.is_file() and (base / "coriolis-ansible").is_file()
    result["updater_sha256"] = check("updater digest", lambda: digest(updater.read_bytes())) or ""
    config_paths = [
        "/etc/coriolis/coriolis.conf", "/etc/kolla/passwords.yml", "/etc/kolla/globals.yml",
        str(base / "config.yml"), str(base / "docker-images-config.yml"),
    ]
    def configuration_digest(paths):
        return digest(b"".join(p.encode() + b"\0" + pathlib.Path(p).read_bytes() + b"\0" for p in paths))
    result["config_sha256"] = check("configuration digest", lambda: configuration_digest(config_paths)) or ""
    result["protected_sha256"] = check("protected settings digest", lambda: configuration_digest(config_paths[:2])) or ""
    result["update_locked"] = pathlib.Path("/var/lib/coriolis-stackit/update-lock").exists()
    names = check("container discovery", lambda: command(["docker", "ps", "-aq"]).split())
    if not names:
        result["errors"].append("no appliance containers found")
    else:
        containers = check("container inspection", lambda: json.loads(command(["docker", "inspect"] + names)))
        if containers is not None:
            local_filesystems = {"ext2", "ext3", "ext4", "xfs", "btrfs", "tmpfs", "devtmpfs", "overlay"}
            sources = {mount["Source"] for c in containers for mount in c.get("Mounts", [])}
            for source in sorted(sources):
                fs = check("persistent storage inspection", lambda: command(["findmnt", "-n", "-o", "FSTYPE", "-T", source]).strip())
                if fs is not None and fs not in local_filesystems:
                    result["errors"].append("container mount is not on a supported local filesystem")
            result["containers"] = [
                {"name": c["Name"].lstrip("/"), "image": c["Config"]["Image"],
                 "image_id": c["Image"], "state": c["State"]["Status"],
                 "health": c["State"].get("Health", {}).get("Status", "none")}
                for c in sorted(containers, key=lambda value: value["Name"])
            ]
            for c in result["containers"]:
                if c["state"] != "running" or c["health"] not in ("healthy", "none"):
                    result["errors"].append("container not ready: " + c["name"])
    check("identity login", lambda: command(["openstack", "token", "issue", "-f", "value", "-c", "expires"]))

    # Query the database rather than a project-scoped CLI list: maintenance
    # must account for executions and schedules belonging to every tenant.
    local_ip = check("local address", lambda: command(["ip", "-4", "route", "get", "1.1.1.1"]).split("src ", 1)[1].split()[0])
    database_script = CORIOLIS_DATABASE_SCRIPT
    db = check("global data and workload inspection", lambda: json.loads(
        subprocess.run(["docker", "exec", "-i", "coriolis-conductor", "python3", "-", local_ip or ""],
                       input=database_script, check=True, text=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90).stdout))
    if db is not None:
        result.update(db)

    fqdn = sys.argv[1]
    if fqdn and local_ip:
        check("HTTPS API with trusted certificate", lambda: command([
            "curl", "--fail", "--silent", "--show-error", "--connect-timeout", "5",
            "--max-time", "15", "--resolve", fqdn + ":443:" + local_ip,
            "https://" + fqdn + "/api/config",
        ]))
        fingerprints = {}
        for port in (443, 5000):
            def fingerprint():
                context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                context.check_hostname = False
                context.verify_mode = ssl.CERT_NONE
                with socket.create_connection((local_ip, port), timeout=10) as raw:
                    with context.wrap_socket(raw, server_hostname=fqdn) as tls:
                        return digest(tls.getpeercert(binary_form=True))
            value = check("TLS fingerprint on " + str(port), fingerprint)
            if value:
                fingerprints[str(port)] = value
        result["tls_fingerprints"] = fingerprints
    else:
        check("local web API", lambda: command([
            "curl", "--fail", "--silent", "--show-error", "--insecure",
            "--connect-timeout", "5", "--max-time", "15", "https://127.0.0.1/api/config",
        ]))
    result["healthy"] = not result["errors"]
    encoded = json.dumps(result, separators=(",", ":"))
    if len(encoded.encode()) > 9500:
        raise ValueError("status exceeds Run Command output window")
    print(encoded)


try:
    collect()
except (OSError, ValueError, KeyError, IndexError, subprocess.SubprocessError) as error:
    print(json.dumps({"healthy": False, "errors": ["status collection failed: " + type(error).__name__]}))
    sys.exit(1)
