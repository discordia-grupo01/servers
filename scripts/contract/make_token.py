#!/usr/bin/env python3
"""Imprime un JWT de prueba sin firma real para el contrato (solo biblioteca estandar).

servers no verifica la firma (la valida Kong): solo lee el `sub` del payload.
Uso: make_token.py <sub-uuid>
"""
import base64
import json
import sys


def b64(data: dict) -> str:
    return base64.urlsafe_b64encode(json.dumps(data, separators=(",", ":")).encode()).rstrip(b"=").decode()


def token(sub: str) -> str:
    return b64({"alg": "none", "typ": "JWT"}) + "." + b64({"sub": sub}) + ".contract"


def main() -> int:
    if len(sys.argv) < 2:
        print("uso: make_token.py <sub-uuid>", file=sys.stderr)
        return 2
    print(token(sys.argv[1]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
