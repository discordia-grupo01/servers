"""Escenario de camino feliz para el test de contrato de servers.

Schemathesis, con datos al azar, no llega al 2xx de operaciones que piden un
cuerpo muy especifico (crear servidor con nombre valido, reordenar con la lista
completa, silenciar con fecha futura, etc.). Este escenario las recorre con
datos validos contra el servicio real, comprueba el status esperado y escribe un
HAR minimo para medir la cobertura (coverage.py).

Uso: scenario.py <openapi.yaml> <base_url> <har_de_salida>
Solo usa valores de juguete y habla con localhost.
"""

import json
import sys
import uuid
from urllib.parse import urlparse

from make_token import token
from seed import PNG, call

_, BASE, HAR_OUT = sys.argv[1:4]
if urlparse(BASE).hostname not in ("localhost", "127.0.0.1", "::1"):
    raise SystemExit("scenario: solo corre contra localhost")

RUN = uuid.uuid4().hex[:8]
OWNER_ID = "00000000-0000-4000-8000-0000000000d1"
OWNER = token(OWNER_ID)
MEMBERS = [f"00000000-0000-4000-8000-0000000000d{n}" for n in (2, 3, 4, 5)]
entries = []
failures = []


def step(method, path, expect, **kwargs):
    """Llama, registra en el HAR y anota una falla si el status no es el esperado."""
    tok = kwargs.pop("tok", OWNER)
    status, body = call(BASE, method, path, tok, **kwargs)
    entries.append({"request": {"method": method, "url": BASE + path}, "response": {"status": status}})
    if status not in (expect if isinstance(expect, tuple) else (expect,)):
        failures.append(f"{method} {path}: se esperaba {expect} y llego {status}: {str(body)[:200]}")
    return body


def main():
    name = f"scenario-{RUN}"
    server = step("POST", "/v1/servers", 201, form={"name": name},
                  files={"icon": ("icon.png", PNG, "image/png")})
    sid = server["id"]
    invite = step("POST", f"/v1/servers/{sid}/invites", 201, body={})["code"]
    for u in MEMBERS:
        step("POST", f"/v1/invites/{invite}/join", 201, tok=token(u))

    # Reordenar con la lista completa y vigente.
    cat = step("POST", f"/v1/servers/{sid}/categories", 201, body={"name": "extra"})
    cats = step("GET", f"/v1/servers/{sid}", 200)["categories"]
    step("PATCH", f"/v1/categories/{cat['id']}", 200, body={"name": "renombrada"})
    step("PATCH", f"/v1/servers/{sid}/categories/reorder", 200, body={"category_ids": [c["id"] for c in reversed(cats)]})
    role = step("POST", f"/v1/servers/{sid}/roles", 201, body={"name": "r1", "color": "#112233"})
    role2 = step("POST", f"/v1/servers/{sid}/roles", 201, body={"name": "r2", "color": "#112233"})
    step("PATCH", f"/v1/servers/{sid}/roles/reorder", 200, body={"role_ids": [role2["id"], role["id"]]})
    step("POST", f"/v1/servers/{sid}/members/{MEMBERS[0]}/roles", 200, body={"role_id": role["id"]})

    # Voz: el dueno puede conectarse a un canal de voz.
    voice = step("POST", f"/v1/servers/{sid}/channels", 201, body={"name": "voz", "kind": "voice"})
    step("GET", f"/v1/channels/{voice['id']}/voice-access", 200)
    text = step("POST", f"/v1/servers/{sid}/channels", 201, body={"name": "texto", "kind": "text"})
    step("POST", f"/v1/servers/{sid}/mentions/resolve", 200,
         body={"channel_id": text["id"], "user_ids": [MEMBERS[0]], "role_ids": [role["id"]], "everyone": False})

    # PATCH/PUT con cuerpo valido: el fuzzing los alcanza segun lo que genere.
    step("PATCH", f"/v1/channels/{text['id']}", 200, body={"name": "texto-2", "topic": "tema"})
    step("PATCH", f"/v1/channels/{text['id']}/category", 200, body={"category_id": cat["id"]})
    step("PATCH", f"/v1/roles/{role['id']}", 200, body={"name": "r1-b", "color": "#445566"})
    step("PUT", f"/v1/servers/{sid}/default-role", 204, body={"role_id": role["id"]})
    step("PUT", f"/v1/servers/{sid}/banned-words", 200, body={"words": ["spam"]})
    step("PATCH", f"/v1/servers/{sid}", 200, form={"name": name}, files={"icon": ("icon.png", PNG, "image/png")})

    # DELETE de un solo uso: cada recurso solo da 2xx una vez, asi que el fuzzing los alcanza de
    # forma inestable. Se crean y se borran aca para que la cobertura no dependa del azar.
    step("PUT", f"/v1/channels/{text['id']}/permissions/roles/{role['id']}", (200, 204),
         body={"allow": ["SEND_MESSAGES"], "deny": []})
    step("DELETE", f"/v1/channels/{text['id']}/permissions/roles/{role['id']}", 204)
    step("DELETE", f"/v1/servers/{sid}/members/{MEMBERS[0]}/roles/{role['id']}", 204)
    emoji = step("POST", f"/v1/servers/{sid}/emojis", 201, form={"name": f"emoji_{RUN}"},
                 files={"image": ("e.png", PNG, "image/png")})
    step("DELETE", f"/v1/servers/{sid}/emojis/{emoji['id']}", 204)
    tmp_channel = step("POST", f"/v1/servers/{sid}/channels", 201, body={"name": "tmp", "kind": "text"})
    step("DELETE", f"/v1/channels/{tmp_channel['id']}", 204)
    tmp_role = step("POST", f"/v1/servers/{sid}/roles", 201, body={"name": "tmp", "color": "#112233"})
    step("DELETE", f"/v1/roles/{tmp_role['id']}", 204)
    tmp_invite = step("POST", f"/v1/servers/{sid}/invites", 201, body={})["code"]
    step("DELETE", f"/v1/invites/{tmp_invite}", 204)

    # Moderacion.
    step("POST", f"/v1/servers/{sid}/bans", 201, body={"user_id": MEMBERS[2]})
    step("DELETE", f"/v1/servers/{sid}/bans/{MEMBERS[2]}", 204)
    step("POST", f"/v1/servers/{sid}/mutes", 204, body={"user_id": MEMBERS[0], "until_at": "2999-01-01T00:00:00Z"})
    step("DELETE", f"/v1/servers/{sid}/mutes/{MEMBERS[0]}", 204)
    report = step("POST", f"/v1/servers/{sid}/reports", 201,
                  body={"kind": "user", "user_id": MEMBERS[1], "reason": "scenario"}, tok=token(MEMBERS[0]))
    step("PATCH", f"/v1/servers/{sid}/reports/{report['id']}", 200, body={"status": "dismissed"})

    # Transferencia: crear, ver la pendiente y cancelarla; se deja una sin responder.
    first = step("POST", f"/v1/servers/{sid}/ownership-transfers", 201, body={"to_user_id": MEMBERS[0]})
    step("GET", f"/v1/servers/{sid}/ownership-transfers/pending", 200)
    step("POST", f"/v1/servers/{sid}/ownership-transfers/{first['id']}/cancel", 200)
    second = step("POST", f"/v1/servers/{sid}/ownership-transfers", 201, body={"to_user_id": MEMBERS[0]})
    step("POST", f"/v1/servers/{sid}/ownership-transfers/{second['id']}/reject", 200, tok=token(MEMBERS[0]))
    transfer = step("POST", f"/v1/servers/{sid}/ownership-transfers", 201, body={"to_user_id": MEMBERS[0]})

    # Expulsar al ultimo (el rol alto es del dueno: queda por debajo).
    step("POST", f"/v1/servers/{sid}/kicks", 204, body={"user_id": MEMBERS[1]})
    # Un miembro abandona el servidor.
    step("DELETE", f"/v1/servers/{sid}/members/{MEMBERS[3]}", 204, tok=token(MEMBERS[3]))

    # Aceptar la transferencia al final: el destinatario pasa a ser el dueno.
    step("POST", f"/v1/servers/{sid}/ownership-transfers/{transfer['id']}/accept", 200, tok=token(MEMBERS[0]))
    # El nuevo dueno borra el servidor (pide el nombre como confirmacion).
    step("DELETE", f"/v1/servers/{sid}", 204, tok=token(MEMBERS[0]), body={"confirm_name": name})


try:
    main()
finally:
    with open(HAR_OUT, "w", encoding="utf-8") as f:
        json.dump({"log": {"entries": entries}}, f)

if failures:
    print("scenario: fallas en el camino feliz:", file=sys.stderr)
    for failure in failures:
        print("  - " + failure, file=sys.stderr)
    sys.exit(1)
print(f"scenario: {len(entries)} llamadas con el status esperado")
