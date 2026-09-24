# discordia-servers

Servicio de **Servidores** de Discordia (servidores, canales, roles/permisos,
moderación). Historias cubiertas hasta ahora:

- **Crear servidor**.
- **Generar invitación** (link único, expira a los 7 días, con o sin límite de usos; revocable).
- **Unirse a servidor vía invitación**.
- **Abandonar servidor** 
- **Transferir propiedad del servidor**
- **Crear rol** (#21).
- **Editar permisos de un rol** (#22).
- **Asignar/quitar rol a un miembro** (#23).
- **Definir rol por defecto para nuevos miembros** (#24).
- **Eliminar rol** (optativa).
- **Crear/editar/eliminar canal**.
- **Organizar canales en categorías** (crear categoría, mover un canal a una categoría o sacarlo).
- **Reordenar canales dentro de una categoría, y categorías entre sí** (optativa).

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
| DATABASE_SSLMODE | disable | `disable` en local; `require` cuando la BDD es Supabase (exige TLS). |
| DATABASE_SCHEMA | - (vacio) | Vacio en local. En Supabase, el schema dedicado de este servicio dentro del proyecto compartido (ver seccion "Supabase" mas abajo). |
| USER_ID_HEADER | X-User-Id | Header con el user id que inyecta Kong tras validar el JWT. |
| ICON_DIR | ./data/icons | Directorio donde se guardan los iconos subidos (si no se usa Supabase Storage, ver debajo). |
| ICON_MAX_BYTES | 5242880 | Tamano maximo (bytes) de un icono o banner. |
| SUPABASE_STORAGE_URL | - (vacio) | URL del proyecto de Supabase (`https://xxxx.supabase.co`). Si esta + `SUPABASE_STORAGE_BUCKET` + `SUPABASE_SERVICE_KEY`, los iconos van a ese bucket en vez de a `ICON_DIR` (ver seccion "Supabase Storage" mas abajo). |
| SUPABASE_STORAGE_BUCKET | - (vacio) | Nombre del bucket de Storage. |
| SUPABASE_SERVICE_KEY | - (vacio) | `service_role` key del proyecto (Project Settings > API) -- nunca la `anon` key, esta se salta las políticas RLS del bucket. |
| RABBITMQ_URL | - (vacio) | Conexion al broker para publicar eventos de dominio (exchange `discordia.events`, ver ADR-07). Vacia = publisher en no-op, el servicio funciona igual sin RabbitMQ. |

Las `DATABASE_*` no tienen default: si falta alguna, el servicio no arranca (no se usa una credencial adivinada). Los valores reales van en el `.env`; `.env.example` trae los del Postgres local.

## Estructura de carpetas
```
servers/
├── cmd/
│   └── api/
│       └── main.go          entrypoint: arma el router y levanta el server
├── internal/
│   ├── handler/              handlers HTTP (reciben el request, parsean, llaman a service)
│   ├── service/               logica de negocio
│   ├── repository/            acceso a datos (Postgres, etc.)
│   ├── model/                 structs de dominio / DTOs
│   ├── middleware/            middlewares de Gin (auth, logging, etc.)
│   └── events/                 publisher de eventos hacia RabbitMQ (exchange discordia.events)
├── migrations/                migraciones de BDD (ej. golang-migrate)
├── .github/
│   └── workflows/
│       └── ci.yml            gofmt, build, vet y test (con Postgres y RabbitMQ reales) en cada PR/push a main
├── .githooks/
│   └── pre-commit             hook de gofmt local
├── openapi.yaml               contrato de la API REST -- generado (make openapi), no se edita a mano
├── docker-compose.yml         levanta el servicio + Postgres para desarrollo local
├── Dockerfile                 build multi-stage para la imagen de produccion
├── Makefile                   comandos run/test/build/migrate/openapi
└── .env.example                estructura de variables de entorno (sin valores sensibles)
```

## Comandos (Makefile)
- `make run` - corre el servicio localmente.
- `make test` - corre los tests con cobertura.
- `make build` - compila el binario en `bin/api`.
- `make db-reset` - recrea la base desde cero (borra el volumen).
- `make migrate` - aplica `migrations/*.up.sql` en orden contra la BDD que indique `.env` (local o Supabase), vía `golang-migrate`. Trackea en la propia BDD cuales ya se aplicaron: correrlo de nuevo despues de la primera vez es seguro, solo aplica las migraciones nuevas.
- `make openapi` - regenera `openapi.yaml` desde las anotaciones swag de los handlers (`internal/handler/**`). Necesita Go y Node. No hace falta correrlo a mano: el CI lo corre en cada push/PR y, si detecta que cambió, commitea `openapi.yaml` solo de vuelta a la rama.

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
`test/repository/role_repo`, `test/repository/member_repo`,
`test/repository/channel_repo`, `test/repository/category_repo`, etc.) y todos hacen
`DROP TABLE`/recrean el schema contra la misma base al empezar **y al
terminar** — si Go los corre en paralelo (el default), se pisan entre sí.
Por lo mismo, correrlos contra `discordia_servicio` la deja completamente
vacía (sin tablas) al finalizar; si vas a seguir probando el servicio a mano
después, hace falta recrear las tablas (`docker compose down -v && docker
compose up -d db`, o `make migrate` de nuevo).

Los tests de `test/events/publisher_integration_test.go` (incluida la
reconexión real, forzada vía la API de management de RabbitMQ) y
`test/e2e/**` (HTTP real → Postgres real → RabbitMQ real) también son de
integración: se saltean si no hay `RABBITMQ_URL` (`test/e2e` además necesita
`DATABASE_HOST`). No agregamos un servicio `rabbitmq` a este
`docker-compose.yml` (para no chocar de puerto si en algún momento se levanta
junto al compose de `discordia-identify`, que también lo usa) — se levanta un
contenedor suelto para probar:
```bash
docker run --rm -p 5672:5672 -p 15672:15672 rabbitmq:3.13-management-alpine
RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test ./test/events/...
```

**CI** (`.github/workflows/ci.yml`) sí levanta ambos servicios (Postgres y
RabbitMQ) y corre `go test ./... -p 1` con las variables ya seteadas, así que
toda la suite -- unitarios e integración -- corre en cada PR/push, no solo
localmente a mano.

## Supabase (produccion)
El proyecto de Supabase es **compartido entre todos los servicios de Discordia** (un solo proyecto, un schema por servicio). Pasos:
1. Una sola vez por proyecto de Supabase: crear el schema de este servicio. Se puede hacer sin SQL desde **Database > Schemas > New schema** (nombre `servers`), o corriendo en el SQL Editor:
   ```sql
   CREATE SCHEMA IF NOT EXISTS servers;
   ```
2. Completar en tu `.env` los datos de conexion de Supabase, tomados del boton **Connect** del proyecto, pestaña **Session pooler** (no "Direct connection": ese host solo resuelve por IPv6 y muchas redes no tienen esa salida; tampoco "Transaction pooler", que no sostiene el `search_path` por conexion):
   ```
   DATABASE_HOST=aws-0-<region>.pooler.supabase.com
   DATABASE_PORT=5432
   DATABASE_NAME=postgres
   DATABASE_USER=postgres.<project-ref>
   DATABASE_PASSWORD=<password del proyecto>
   DATABASE_SSLMODE=require
   DATABASE_SCHEMA=servers
   ```
3. Correr `make migrate` y despues `make run`. Confirmar con `GET /readyz` (200).

## Endpoints
Ver [`openapi.yaml`](openapi.yaml) -- **generado automáticamente y actualizado
solo**, no se edita a mano ni hace falta correr nada vos. Sale de las
anotaciones `@Summary`/`@Param`/`@Success`/etc. (swag) arriba de cada handler
en `internal/handler/**`. En cada push/PR, el CI corre `make openapi`
(swag → swagger2openapi, de Swagger 2.0 a OpenAPI 3.0) y si el resultado
cambió respecto a lo commiteado, lo commitea de vuelta a la rama solo (commit
`docs(openapi): regenerar openapi.yaml`, con `[skip ci]` para no disparar un
run nuevo). Si querés verlo vos antes de pushear, `make openapi` local hace
lo mismo (necesita Go y Node).

La tabla de abajo es una referencia rápida (puede quedar desactualizada
antes que `openapi.yaml`, que es la fuente de verdad real).

| Metodo | Ruta | Auth | Descripcion |
|---|---|---|---|
| POST | `/v1/servers` | si | Crear servidor (multipart: `name`, `icon?`). Sin `icon`, se genera un ícono default de color random. |
| GET | `/v1/servers` | si | Mis servidores (de los que soy owner o miembro), con sus canales y categorias. |
| GET | `/v1/servers/{serverId}` | si | Un servidor puntual (nombre, canales, categorias, icon_url, banner_url). Ej.: después de un `join`, para obtener los datos con solo el `server_id`. Cada canal trae su `category_id` (`null` si esta "sin categoria"). |
| PATCH | `/v1/servers/{serverId}` | si (owner) | Editar nombre, ícono y/o banner (multipart: `name?`, `icon?`, `banner?`, `remove_banner?`). Todos los campos son opcionales; sin permisos da 403, datos invalidos dan 400 sin persistir nada. |
| GET | `/v1/servers/{serverId}/icon` | no (público) | Sirve el archivo del ícono (subido o el default generado). En produccion (Supabase) `icon_url` apunta directo al bucket público; esta ruta queda como fallback para setups sin Supabase. |
| GET | `/v1/servers/{serverId}/banner` | no (público) | Sirve el archivo del banner. 404 si el server no tiene uno. Mismo esquema de URL que el icono. |
| GET | `/v1/servers/{serverId}/members` | si | Listar los miembros del server (paginado). |
| POST | `/v1/servers/:serverId/invites` | si | Generar invitación (JSON opcional: `max_uses?`; expira a los 7 días). |
| GET | `/v1/servers/:serverId/invites` | si (miembro) | Listar todas las invitaciones del server (activas, revocadas o vencidas), más nueva primero. Sin filtro por estado del lado del back. |
| DELETE | `/v1/invites/:code` | si | Revocar una invitación (idempotente). |
| POST | `/v1/invites/:code/join` | si | Unirse a un servidor con una invitación. |
| DELETE | `/v1/servers/{serverId}/members/{userId}` | si (uno mismo) | Abandonar un servidor. `userId` debe ser el propio caller; el owner no puede abandonar así (409, primero tiene que transferir la propiedad o eliminar el servidor). |
| POST | `/v1/servers/{serverId}/ownership-transfers` | si (owner) | Ofrecer la propiedad a otro miembro (`to_user_id`). Crea una transferencia pendiente; no cambia nada hasta que el destinatario responde. Rechaza si `to_user_id` no es miembro (400) o si ya hay una transferencia pendiente para ese server (409). |
| GET | `/v1/servers/{serverId}/ownership-transfers/pending` | si (miembro) | Ver la transferencia pendiente del server, si hay una. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/accept` | si (destinatario) | Aceptar: pasa a ser el nuevo owner y el anterior queda como miembro regular. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/reject` | si (destinatario) | Rechazar: la propiedad no cambia. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/cancel` | si (owner que la inicio) | Cancelar la transferencia pendiente antes de que el destinatario responda (p. ej. para ofrecersela a otro miembro). |
| POST | `/v1/servers/{serverId}/roles` | si (owner) | Crear rol (`name`, `color`). |
| GET | `/v1/servers/{serverId}/roles` | si | Listar roles de un server. |
| GET | `/v1/roles/{roleId}` | si | Un rol puntual (mismo shape que un elemento de `GET /v1/servers/{serverId}/roles`). 404 si no existe. |
| PATCH | `/v1/roles/{roleId}` | si (owner) | Editar permisos/nombre/color de un rol. |
| POST | `/v1/servers/{serverId}/members/{userId}/roles` | si (owner) | Asignar un rol a un miembro (idempotente). |
| DELETE | `/v1/servers/{serverId}/members/{userId}/roles/{roleId}` | si (owner) | Quitar un rol de un miembro. |
| GET | `/v1/servers/{serverId}/members/{userId}/roles` | si | Listar los roles de un miembro. |
| GET | `/v1/servers/{serverId}/members/{userId}/permissions` | si | Permisos efectivos del miembro (union de los permisos de todos sus roles). |
| PUT | `/v1/servers/{serverId}/default-role` | si (owner) | Definir el rol por defecto del server. |
| DELETE | `/v1/roles/{roleId}` | si (owner) | Eliminar un rol (optativa). Limpia asignaciones; rechaza con 409 si el rol es el default actual del server (hay que asignar otro default primero). |
| POST | `/v1/servers/{serverId}/channels` | si (owner) | Crear canal de texto o voz (`name`, `kind`, `category_id?`). 400 si el nombre esta vacio o ya existe otro canal con ese nombre en la misma categoria (o en el "sin categoria", segun corresponda). |
| PATCH | `/v1/channels/{channelId}` | si (owner) | Editar nombre y/o topic de un canal. |
| DELETE | `/v1/channels/{channelId}` | si (owner) | Eliminar un canal. |
| POST | `/v1/servers/{serverId}/categories` | si (owner) | Crear categoria (`name`). |
| PATCH | `/v1/channels/{channelId}/category` | si (owner) | Mover un canal a una categoria (`category_id`), o sacarlo a "sin categoria" (`category_id: null`). |
| PATCH | `/v1/servers/{serverId}/channels/reorder` | si (owner) | Reordenar los canales de una categoria (`category_id?`, `channel_ids`: el orden completo y exacto de esa categoria). 400 si la lista no coincide exactamente con los canales actuales de esa categoria. |
| PATCH | `/v1/servers/{serverId}/categories/reorder` | si (owner) | Reordenar las categorias del server (`category_ids`: el orden completo y exacto). 400 si la lista no coincide exactamente. |
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
