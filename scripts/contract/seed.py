#!/usr/bin/env python3
"""Siembra datos de prueba en el servicio levantado (solo biblioteca estandar).

Crea, por la API y con identidades de juguete, un servidor con canales, rol,
categoria, invitacion, emoji, miembros y una transferencia pendiente, para que
Schemathesis use ids reales en vez de inventados. Imprime `export NOMBRE=valor`
(uno por linea) con los ids; el script de contrato los exporta y el toml los
referencia con ${NOMBRE}.

Uso: seed.py <base-url> <user-id-owner>
Solo habla con localhost. Los tokens son JWT sin firma real (servers no la verifica).
"""
import base64
import json
import sys
import urllib.error
import urllib.request
import uuid
from urllib.parse import urlparse

from make_token import token

# PNG de 1x1 pixel.
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)


def call(base, method, path, token, body=None, form=None, files=None):
    headers = {"Authorization": "Bearer " + token}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    elif form is not None:
        boundary = "contractseed" + uuid.uuid4().hex
        parts = []
        for k, v in form.items():
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
        for k, (fname, content, ctype) in (files or {}).items():
            parts.append(
                (f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"; filename="{fname}"\r\n'
                 f"Content-Type: {ctype}\r\n\r\n").encode() + content + b"\r\n"
            )
        parts.append(f"--{boundary}--\r\n".encode())
        data = b"".join(parts)
        headers["Content-Type"] = "multipart/form-data; boundary=" + boundary
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw


def must(res, what, ok=(200, 201, 204)):
    status, body = res
    if status not in ok:
        print(f"seed: {what} devolvio {status}: {body}", file=sys.stderr)
        sys.exit(1)
    return body


def main():
    base, owner_id = sys.argv[1:3]
    host = urlparse(base).hostname
    if host not in ("localhost", "127.0.0.1", "::1"):
        print("seed: solo corre contra localhost", file=sys.stderr)
        return 1
    owner = token(owner_id)
    # Usuarios de juguete: [0] queda como miembro "comun" (silenciado, con rol,
    # destinatario de la transferencia), [1] abandona, [2] se expulsa, [3] se banea.
    users = [f"00000000-0000-4000-8000-0000000000c{n}" for n in (2, 3, 4, 5)]
    tokens = [token(u) for u in users]
    out = {}
    run = uuid.uuid4().hex[:8]  # el nombre es unico por dueno: se puede correr mas de una vez
    out["SEED_RUN"] = run

    def server(name):
        full = f"{name}-{run}"
        sid = must(call(base, "POST", "/v1/servers", owner, form={"name": full},
                        files={"icon": ("icon.png", PNG, "image/png")}), "crear servidor")["id"]
        return sid, full

    s, out["SEED_SERVER_NAME"] = server("contract-main")
    out["SEED_SERVER"] = s
    out["SEED_SERVER_DEL"], out["SEED_SERVER_DEL_NAME"] = server("contract-del")

    def channel(name, kind="text"):
        return must(call(base, "POST", f"/v1/servers/{s}/channels", owner, body={"name": name, "kind": kind}),
                    "crear canal")["id"]

    out["SEED_CHANNEL"] = channel("seed-text")
    out["SEED_VOICE"] = channel("seed-voice", "voice")
    out["SEED_CATEGORY"] = must(call(base, "POST", f"/v1/servers/{s}/categories", owner, body={"name": "seed-cat"}),
                                "crear categoria")["id"]
    out["SEED_ROLE"] = must(call(base, "POST", f"/v1/servers/{s}/roles", owner,
                                 body={"name": "seed-role", "color": "#112233"}), "crear rol")["id"]

    def invite():
        return must(call(base, "POST", f"/v1/servers/{s}/invites", owner, body={}), "crear invitacion")["code"]

    out["SEED_INVITE"] = invite()
    for n, tk in enumerate(tokens):
        must(call(base, "POST", f"/v1/invites/{invite()}/join", tk), f"unirse ({n})")
    out["SEED_MEMBER"], out["SEED_LEAVER"], out["SEED_KICK"], out["SEED_BAN"] = users
    out["SEED_LEAVER_TOKEN"] = tokens[1]
    out["SEED_MEMBER_TOKEN"] = tokens[0]
    e = must(call(base, "POST", f"/v1/servers/{s}/emojis", owner, form={"name": "seed_emoji"},
                  files={"image": ("e.png", PNG, "image/png")}), "crear emoji")["id"]
    out["SEED_EMOJI"] = e
    rep = must(call(base, "POST", f"/v1/servers/{s}/reports", tokens[0],
                    body={"kind": "user", "user_id": users[1], "reason": "seed"}), "crear reporte")
    out["SEED_REPORT"] = rep["id"]
    tr = must(call(base, "POST", f"/v1/servers/{s}/ownership-transfers", owner, body={"to_user_id": users[0]}),
              "iniciar transferencia")
    out["SEED_TRANSFER"] = tr["id"]
    # Estado para las operaciones que deshacen algo (DELETE): override de canal,
    # rol asignado, silencio y un baneo.
    must(call(base, "PUT", f"/v1/channels/{out['SEED_CHANNEL']}/permissions/roles/{out['SEED_ROLE']}", owner,
              body={"allow": ["SEND_MESSAGES"], "deny": []}), "override de canal")
    must(call(base, "POST", f"/v1/servers/{s}/members/{users[0]}/roles", owner, body={"role_id": out["SEED_ROLE"]}),
         "asignar rol")
    must(call(base, "POST", f"/v1/servers/{s}/mutes", owner,
              body={"user_id": users[0], "until_at": "2999-01-01T00:00:00Z"}), "silenciar")
    must(call(base, "POST", f"/v1/servers/{s}/bans", owner, body={"user_id": users[3]}), "banear")
    for k, v in out.items():
        print(f"export {k}={v}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
