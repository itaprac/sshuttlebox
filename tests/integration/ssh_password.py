#!/usr/bin/env python3
"""Test the real OpenSSH client against a local SFTP authentication fixture.

Requires Paramiko in the Python environment. Build shbx, then run:
    python tests/integration/ssh_password.py /absolute/path/to/shbx

The fixture uses a loopback port, generated host key, fake credentials and a
separate HOME. It does not read or change the user's SSH configuration.
"""

import json
import logging
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time

import paramiko


def run(binary: Path, root: Path) -> None:
    key = paramiko.RSAKey.generate(2048)
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen()
    port = listener.getsockname()[1]
    password_attempts = []
    files = root / "files"
    files.mkdir()
    (files / "authenticated.txt").write_text("download fixture\n")

    class Server(paramiko.ServerInterface):
        def get_allowed_auths(self, username):
            return "keyboard-interactive" if username == "keyboard" else "password"

        def check_auth_password(self, username, password):
            password_attempts.append(username)
            if username == "test" and password == "fixture-secret":
                return paramiko.AUTH_SUCCESSFUL
            return paramiko.AUTH_FAILED

        def check_auth_interactive(self, username, submethods):
            self.username = username
            return paramiko.InteractiveQuery("Password", "", ("Password: ", False))

        def check_auth_interactive_response(self, responses):
            password_attempts.append(self.username)
            if self.username == "keyboard" and responses == ["fixture-secret"]:
                return paramiko.AUTH_SUCCESSFUL
            return paramiko.AUTH_FAILED

        def check_channel_request(self, kind, chanid):
            if kind == "session":
                return paramiko.OPEN_SUCCEEDED
            return paramiko.OPEN_FAILED_ADMINISTRATIVELY_PROHIBITED

    class SFTP(paramiko.SFTPServerInterface):
        def local_path(self, path):
            target = (files / path.lstrip("/")).resolve()
            if not target.is_relative_to(files):
                raise OSError("path outside fixture")
            return target

        def list_folder(self, path):
            entries = []
            for child in self.local_path(path).iterdir():
                attrs = paramiko.SFTPAttributes.from_stat(child.stat())
                attrs.filename = child.name
                entries.append(attrs)
            return entries

        def stat(self, path):
            try:
                return paramiko.SFTPAttributes.from_stat(self.local_path(path).stat())
            except OSError as error:
                return paramiko.SFTPServer.convert_errno(error.errno)

        def lstat(self, path):
            return self.stat(path)

        def canonicalize(self, path):
            relative = self.local_path(path).relative_to(files)
            return "/" if relative == Path(".") else "/" + relative.as_posix()

        def open(self, path, flags, attr):
            try:
                descriptor = os.open(self.local_path(path), flags, attr.st_mode or 0o600)
                mode = "rb" if flags & (os.O_WRONLY | os.O_RDWR) == 0 else "wb"
                stream = os.fdopen(descriptor, mode)
                handle = paramiko.SFTPHandle(flags)
                if mode == "rb":
                    handle.readfile = stream
                else:
                    handle.writefile = stream
                return handle
            except OSError as error:
                return paramiko.SFTPServer.convert_errno(error.errno)

    transports = []

    def handle(connection):
        transport = paramiko.Transport(connection)
        transports.append(transport)
        transport.add_server_key(key)
        transport.set_subsystem_handler("sftp", paramiko.SFTPServer, SFTP)
        try:
            transport.start_server(server=Server())
            while transport.is_active():
                time.sleep(0.02)
        except (EOFError, OSError):
            pass
        finally:
            transport.close()

    def accept():
        while True:
            try:
                connection, _ = listener.accept()
            except OSError:
                return
            threading.Thread(target=handle, args=(connection,), daemon=True).start()

    threading.Thread(target=accept, daemon=True).start()
    known_hosts = root / "known_hosts"
    known_key = f"[127.0.0.1]:{port} {key.get_name()} {key.get_base64()}\n"
    ssh_config = root / "ssh_config"
    ssh_config.write_text(
        f"Host fixture\n HostName 127.0.0.1\n Port {port}\n User test\n"
        f" UserKnownHostsFile {known_hosts}\n GlobalKnownHostsFile /dev/null\n"
        " PubkeyAuthentication no\n PasswordAuthentication yes\n"
    )
    config_path = root / ".config/sshuttlebox/config.json"
    config_path.parent.mkdir(parents=True)
    environment = os.environ.copy()
    environment["HOME"] = str(root)
    for variable in ("SHBX_SSH_BIN", "SHBX_SFTP_BIN"):
        environment.pop(variable, None)
    upload = root / "upload.txt"
    upload.write_text("upload fixture\n")
    download = root / "download.txt"
    cases = [
        ("ls", "fixture-secret", True, ["ls", "fixture", "/"]),
        ("get", "fixture-secret", True, ["get", "fixture", "/authenticated.txt", str(download)]),
        ("put", "fixture-secret", True, ["put", "fixture", str(upload), "/uploaded.txt"]),
        ("keyboard password", "fixture-secret", True, ["ls", "fixture", "/"]),
        ("wrong password", "wrong-secret", True, ["ls", "fixture", "/"]),
        ("unknown host key", "fixture-secret", False, ["ls", "fixture", "/"]),
    ]
    try:
        for name, password, trusted, arguments in cases:
            known_hosts.write_text(known_key if trusted else "")
            config_path.write_text(json.dumps({
                "version": 1,
                "hosts": {"fixture": {"host": "fixture", "sshConfigFile": str(ssh_config), "password": password, "user": "keyboard" if name == "keyboard password" else "test"}},
                "tunnels": {}, "groups": {},
            }))
            before = len(password_attempts)
            started = time.monotonic()
            result = subprocess.run(
                [str(binary), "sftp", *arguments], env=environment,
                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=10,
            )
            attempts = len(password_attempts) - before
            success = name in ("ls", "get", "put", "keyboard password")
            assert (result.returncode == 0) == success, (name, result.stdout, result.stderr)
            assert attempts == (1 if trusted else 0), (name, attempts)
            if name == "ls":
                assert "authenticated.txt" in result.stdout, result.stdout
            elif name == "get":
                assert download.read_text() == "download fixture\n"
            elif name == "put":
                assert (files / "uploaded.txt").read_text() == "upload fixture\n"
            print(json.dumps({
                "case": name, "exit": result.returncode, "password_attempts": attempts,
                "elapsed_seconds": round(time.monotonic() - started, 3), "passed": True,
            }), flush=True)
    finally:
        listener.close()
        for transport in transports:
            transport.close()


if __name__ == "__main__":
    logging.getLogger("paramiko").setLevel(logging.CRITICAL)
    if len(sys.argv) != 2:
        raise SystemExit("usage: ssh_password.py /absolute/path/to/shbx")
    with tempfile.TemporaryDirectory(prefix="shbx-auth-integration-") as directory:
        run(Path(sys.argv[1]).resolve(), Path(directory).resolve())
