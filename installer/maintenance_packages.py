import fcntl
import pathlib
import subprocess


def check_package_manager():
    for path in ("/var/lib/dpkg/lock", "/var/lib/dpkg/lock-frontend", "/var/cache/apt/archives/lock"):
        with open(path, "r+") as stream:
            fcntl.lockf(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
    if any(p.name.isdigit() for p in pathlib.Path("/var/lib/dpkg/updates").iterdir()):
        raise RuntimeError("pending dpkg journal; repair package state before maintenance")
    audit = subprocess.run(["dpkg", "--audit"], check=True, text=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    if audit.stdout.strip():
        raise RuntimeError("dpkg audit is not clean; repair package state before maintenance")
    return True
