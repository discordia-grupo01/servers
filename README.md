# discordia-servers

Servicio de **Servidores** de Discordia (servidores, canales, roles/permisos,
moderación). Historias cubiertas hasta ahora:

- **Crear servidor**.
- **Generar invitación** (link único, expira a los 7 días, con o sin límite de usos; revocable).
- **Unirse a servidor vía invitación**.
- **Abandonar servidor** 
- **Crear rol** (#21).
- **Editar permisos de un rol** (#22).
- **Asignar/quitar rol a un miembro** (#23).
- **Definir rol por defecto para nuevos miembros** (#24).
- **Eliminar rol** (optativa).

## Stack
- Lenguaje: Go
- Framework: Gin (github.com/gin-gonic/gin)
- BDD: PostgreSQL (driver `pgx/v5`)
- Auth: el JWT lo valida el gateway (Kong). Este servicio confía en eso y solo
  toma el user id (del header que inyecta Kong, o del claim `sub` del token).
- Infra extra: volumen para iconos

## Setup local
```bash
cp .env.example .env
docker compose up -d db
make run
```
Con `docker compose up` se levanta también el servicio (`api`) contra la base.

## Variables de entorno
| Variable | Default | Descripcion |
|---|---|---|
| ENVIRONMENT | development | `development` o `production`. |
| HOST | 0.0.0.0 | IP donde escucha el servicio. |
| PORT | 8080 | Puerto donde escucha el servicio. |
| DATABASE_HOST | **(obligatoria)** | Host de PostgreSQL. |
| DATABASE_PORT | **(obligatoria)** | Puerto de PostgreSQL. |
| DATABASE_NAME | **(obligatoria)** | Nombre de la base. |
| DATABASE_USER | **(obligatoria)** | Usuario de conexion. |
| DATABASE_PASSWORD | **(obligatoria)** | Password de conexion. |
| USER_ID_HEADER | X-User-Id | Header con el user id que inyecta Kong tras validar el JWT. |
| ICON_DIR | ./data/icons | Directorio donde se guardan los iconos subidos. |
| ICON_MAX_BYTES | 2097152 | Tamano maximo (bytes) de un icono. |

Las `DATABASE_*` no tienen default: si falta alguna, el servicio no arranca (no se usa una credencial adivinada). Los valores reales van en el `.env`; `.env.example` trae los del Postgres local.

## Estructura de carpetas
```
go-template/
├── cmd/
│   └── api/
│       └── main.go          entrypoint: arma el router y levanta el server
├── internal/
│   ├── handler/              handlers HTTP (reciben el request, parsean, llaman a service)
│   ├── service/               logica de negocio
│   ├── repository/            acceso a datos (Postgres, etc.)
│   ├── model/                 structs de dominio / DTOs
│   ├── middleware/            middlewares de Gin (auth, logging, etc.)
│   └── events/                 publish/consume de eventos (RabbitMQ, etc.)
├── migrations/                migraciones de BDD (ej. golang-migrate)
├── .github/
│   └── workflows/
│       └── ci.yml            gofmt, build, vet y test en cada PR/push a main
├── .githooks/
│   └── pre-commit             hook de gofmt local
├── openapi.yaml               contrato de la API REST
├── docker-compose.yml         levanta el servicio + Postgres para desarrollo local
├── Dockerfile                 build multi-stage para la imagen de produccion
├── Makefile                   comandos run/test/build/migrate
└── .env.example                estructura de variables de entorno (sin valores sensibles)
```

## Comandos (Makefile)
- `make run` - corre el servicio localmente.
- `make test` - corre los tests con cobertura.
- `make build` - compila el binario en `bin/api`.
- `make db-reset` - recrea la base desde cero (borra el volumen).

## Tests
```bash
make test
```
Cobertura mínima del proyecto: 70%. Los tests de `test/repository/**` son de
integración: se saltean si no hay `DATABASE_HOST`. Para correrlos:
```bash
docker compose up -d db
DATABASE_HOST=localhost DATABASE_PORT=5432 DATABASE_NAME=discordia_servicio \
  DATABASE_USER=discordia DATABASE_PASSWORD=discordia go test ./... -p 1
```
`-p 1` es necesario: hay más de un paquete de integración (`test/repository`,
`test/repository/role_repo`, `test/repository/member_repo`) y todos hacen
`DROP TABLE`/recrean el schema contra la misma base al empezar **y al
terminar** — si Go los corre en paralelo (el default), se pisan entre sí.
Por lo mismo, correrlos contra `discordia_servicio` la deja completamente
vacía (sin tablas) al finalizar; si vas a seguir probando el servicio a mano
después, hace falta recrear las tablas (`docker compose down -v && docker
compose up -d db`, o reaplicar `migrations/*.sql` a mano).

## Endpoints
Ver [`openapi.yaml`](openapi.yaml).

| Metodo | Ruta | Auth | Descripcion |
|---|---|---|---|
| POST | `/v1/servers` | si | Crear servidor (multipart: `name`, `icon?`). |
| POST | `/v1/servers/:serverId/invites` | si | Generar invitación (JSON opcional: `max_uses?`; expira a los 7 días). |
| DELETE | `/v1/invites/:code` | si | Revocar una invitación (idempotente). |
| POST | `/v1/invites/:code/join` | si | Unirse a un servidor con una invitación. |
| DELETE | `/v1/servers/{serverId}/members/{userId}` | si (uno mismo) | Abandonar un servidor. `userId` debe ser el propio caller; el owner no puede abandonar así (409, primero tiene que transferir la propiedad o eliminar el servidor). |
| POST | `/v1/servers/{serverId}/roles` | si (owner) | Crear rol (`name`, `color`). |
| GET | `/v1/servers/{serverId}/roles` | si | Listar roles de un server. |
| PATCH | `/v1/roles/{roleId}` | si (owner) | Editar permisos/nombre/color de un rol. |
| POST | `/v1/servers/{serverId}/members/{userId}/roles` | si (owner) | Asignar un rol a un miembro (idempotente). |
| DELETE | `/v1/servers/{serverId}/members/{userId}/roles/{roleId}` | si (owner) | Quitar un rol de un miembro. |
| GET | `/v1/servers/{serverId}/members/{userId}/roles` | si | Listar los roles de un miembro. |
| PUT | `/v1/servers/{serverId}/default-role` | si (owner) | Definir el rol por defecto del server. |
| DELETE | `/v1/roles/{roleId}` | si (owner) | Eliminar un rol (optativa). Limpia asignaciones; rechaza con 409 si el rol es el default actual del server (hay que asignar otro default primero). |
| GET | `/livez` `/readyz` | no | Health checks. |

## Health checks
- `GET /livez` - el proceso esta vivo.
- `GET /readyz` - el servicio puede operar (pinguea la base; 503 si no responde).

## Formateo automatico
Este repo se autoformatea con gofmt en cada commit. Activalo una vez por clone:
```bash
git config core.hooksPath .githooks
```
Si usan VS Code con la extension oficial de Go, tambien formatea al guardar (ya configurado en .vscode/settings.json).

El CI corre `gofmt -l` como red de seguridad (falla si encuentra archivos sin formatear), por si alguien clono sin activar el hook.
