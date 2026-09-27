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
- **Reordenar jerarquía de roles** (#56, optativa).
- **Crear/editar/eliminar canal**.
- **Organizar canales en categorías** (crear categoría, mover un canal a una categoría o sacarlo).
- **Reordenar canales dentro de una categoría, y categorías entre sí** (optativa).
- **Banear miembro del servidor** (#25).
- **Revocar baneo** (#26).

## Stack
- Lenguaje: Go
- Framework: Gin (github.com/gin-gonic/gin)
- BDD: PostgreSQL (driver `pgx/v5`)
- Auth: el JWT lo valida el gateway (Kong). Este servicio confía en eso y solo
  toma el user id del claim `sub` del token (el header `X-User-Id` es solo
  para desarrollo local, ver `USER_ID_HEADER`).
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
| USER_ID_HEADER | - (vacio) | Vacio = el usuario sale del `sub` del JWT (`Authorization: Bearer ...`), que es lo que llega via Kong. Solo para desarrollo local (curl/tests): con `X-User-Id` el servicio toma ese header como identidad del usuario. **Nunca setearlo en un entorno desplegado**: Kong no filtra ese header, asi que cualquier cliente podria hacerse pasar por otro usuario. |
| ICON_DIR | ./data/icons | Directorio donde se guardan los iconos subidos (si no se usa Supabase Storage, ver debajo). |
| ICON_MAX_BYTES | 5242880 | Tamano maximo (bytes) de un icono o banner. |
| SUPABASE_STORAGE_URL | - (vacio) | URL del proyecto de Supabase (`https://xxxx.supabase.co`). Si esta + `SUPABASE_STORAGE_BUCKET` + `SUPABASE_SERVICE_KEY`, los iconos van a ese bucket en vez de a `ICON_DIR` (ver seccion "Supabase Storage" mas abajo). |
| SUPABASE_STORAGE_BUCKET | - (vacio) | Nombre del bucket de Storage. |
| SUPABASE_SERVICE_KEY | - (vacio) | `service_role` key del proyecto (Project Settings > API) -- nunca la `anon` key, esta se salta las políticas RLS del bucket. |
| RABBITMQ_URL | - (vacio) | Conexion al broker para publicar eventos de dominio (exchange `discordia.events`, ver ADR-07). Vacia = publisher en no-op, el servicio funciona igual sin RabbitMQ. |

Publicar un evento nunca pasa directo por RabbitMQ desde el request: el
handler escribe una fila en `outbox_events` en la misma transacción que el
cambio de negocio (crear server, sacar un miembro, etc.), y un poller
(`internal/outbox`, arrancado en `main.go`, cada `outbox.PollInterval`) es
el único que efectivamente publica -- así un crash o una caída de RabbitMQ
entre el commit y el publish nunca pierde el evento silenciosamente, solo
lo retrasa. El mensaje además se publica con `DeliveryMode: amqp.Persistent`
(`internal/events/publisher.go`), para que RabbitMQ tampoco lo pierda si el
broker se reinicia con el mensaje todavía sin consumir.

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
│   ├── events/                 publisher de eventos hacia RabbitMQ (exchange discordia.events)
│   └── outbox/                 outbox transaccional: Insert (misma tx que el write de negocio) + Poller
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

### Migraciones en el deploy
En cada push a `main`, el CI corre `make migrate` contra la base de
produccion (job `migrate`, despues de publicar la imagen y **antes** de
cambiarla en la VM). Si una migracion falla, no se despliega. Necesita estos
secrets del repo (Settings > Secrets and variables > Actions), con los mismos
valores que `DATABASE_*` en `~/servers.env` de la VM: `PROD_DATABASE_HOST`,
`PROD_DATABASE_PORT`, `PROD_DATABASE_NAME`, `PROD_DATABASE_USER`,
`PROD_DATABASE_PASSWORD`, `PROD_DATABASE_SSLMODE` y `PROD_DATABASE_SCHEMA`.

Como la migracion se aplica antes del deploy, si despues el smoke test falla y
`deploy.sh` vuelve a la imagen anterior, esa imagen corre contra el schema
nuevo. Por eso las migraciones tienen que ser compatibles con la version
anterior del codigo: agregar columnas nullable o con default, y no renombrar ni
borrar nada en el mismo deploy en que se deja de usar.

## Roles, permisos y jerarquía
Cada rol tiene un conjunto de permisos del catálogo cerrado
(`internal/model/role/permission.go`): `VIEW_CHANNELS`, `SEND_MESSAGES`,
`MANAGE_CHANNELS`, `MANAGE_ROLES`, `KICK_MEMBERS`, `BAN_MEMBERS`,
`MANAGE_SERVER`, `ADMINISTRATOR` y `CREATE_INVITE`. La API siempre usa los
nombres; la máscara de bits es interna.

- **Administradores.** Son el dueño del servidor y los miembros con algún rol
  que tenga `ADMINISTRATOR`. Tienen todos los permisos de forma implícita y
  son los únicos que gestionan roles, canales, categorías y la configuración
  del servidor. Lo que es solo del dueño (transferir la propiedad, no poder
  abandonar el servidor) sigue siendo solo del dueño. Un administrador no-owner
  no puede sacarse `ADMINISTRATOR` a sí mismo (ni editando, ni borrando, ni
  desasignando el rol que se lo da).
- **Permisos de un rol nuevo.** Todo rol se crea con `VIEW_CHANNELS` y
  `SEND_MESSAGES` (`role.DefaultPermissions`); después se editan con
  `PATCH /v1/roles/{roleId}`. Los roles creados antes de este default no se
  tocaron.
- **Los permisos solo suman.** Los permisos de un miembro son la **unión** de
  los de todos sus roles (no hay permisos que "nieguen"). Por eso, quitarle un
  rol no le saca un permiso que también le da otro rol, y los cambios en un rol
  se aplican de inmediato a todos los que lo tienen.
  `GET /v1/servers/{serverId}/members/{userId}/permissions` devuelve los
  permisos **efectivos**, con la misma regla que usa el servicio para
  autorizar: todos para el dueño y los administradores, la unión de sus roles
  para el resto. El cliente puede decidir qué mostrar con un solo "incluye X".
- **Jerarquía.** Cada rol tiene una `position` (1 = el más alto); los roles
  nuevos se crean al final. El rango de un miembro es su rol más alto, y el
  dueño está por encima de todos. Como los permisos solo suman, la jerarquía
  **no resuelve conflictos de permisos** (no los hay): define el **rango**, y
  se usa para:
  - **Reordenar roles** (`PATCH /v1/servers/{serverId}/roles/reorder`, con la
    lista completa de roles, del más alto al más bajo): un administrador
    no-owner solo puede mover los roles que están por debajo de su rol más alto.
  - **Banear**: solo se puede banear a alguien de rango estrictamente menor.

## Moderación
- **Banear** (`POST /v1/servers/{serverId}/bans`, `user_id` y `reason`
  opcional de hasta 512 caracteres): requiere `BAN_MEMBERS` (los
  administradores lo tienen implícito) y que el objetivo sea un miembro de
  rango menor; nunca el dueño ni uno mismo. En una sola transacción se registra
  el baneo, se saca al miembro (con sus roles), se cancela una transferencia de
  propiedad pendiente hacia él y se encolan los eventos `servers.member_left` y
  `servers.member_banned` (sin el motivo). Mientras el baneo esté activo, no
  puede volver a unirse con ninguna invitación.
- **Listar baneos** (`GET /v1/servers/{serverId}/bans`): requiere
  `BAN_MEMBERS`; paginado, más nuevo primero.
- **Revocar un baneo** (`DELETE /v1/servers/{serverId}/bans/{userId}`):
  requiere `BAN_MEMBERS`, sin chequeo de jerarquía (el baneado ya no es
  miembro). Borra el baneo y encola `servers.member_unbanned`. **No** lo vuelve
  a hacer miembro: puede volver a unirse con cualquier invitación válida, y
  entra como miembro nuevo (con el rol por defecto, sin los roles que tenía).

Eventos que publica el servicio (exchange `discordia.events`, contratos en
`github.com/discordia-grupo01/events-contract` y AsyncAPI en `discordia-docs`):
`servers.member_joined`, `servers.member_left`, `servers.member_banned` y
`servers.member_unbanned`.

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
antes que `openapi.yaml`, que es la fuente de verdad real). En la columna
Auth, "admin" es el dueño o un miembro con `ADMINISTRATOR`, y un permiso
(p. ej. `BAN_MEMBERS`) significa que alcanza con tenerlo por algún rol (los
admins lo tienen implícito); ver "Roles, permisos y jerarquía".

| Metodo | Ruta | Auth | Descripcion |
|---|---|---|---|
| POST | `/v1/servers` | si | Crear servidor (multipart: `name`, `icon?`). Sin `icon`, se genera un ícono default de color random. |
| GET | `/v1/servers` | si | Mis servidores (de los que soy owner o miembro), con sus canales y categorias. |
| GET | `/v1/servers/{serverId}` | si | Un servidor puntual (nombre, canales, categorias, icon_url, banner_url). Ej.: después de un `join`, para obtener los datos con solo el `server_id`. Cada canal trae su `category_id` (`null` si esta "sin categoria"). |
| PATCH | `/v1/servers/{serverId}` | si (admin) | Editar nombre, ícono y/o banner (multipart: `name?`, `icon?`, `banner?`, `remove_banner?`). Todos los campos son opcionales; sin permisos da 403, datos invalidos dan 400 sin persistir nada. |
| GET | `/v1/servers/{serverId}/icon` | no (público) | Sirve el archivo del ícono (subido o el default generado). En produccion (Supabase) `icon_url` apunta directo al bucket público; esta ruta queda como fallback para setups sin Supabase. |
| GET | `/v1/servers/{serverId}/banner` | no (público) | Sirve el archivo del banner. 404 si el server no tiene uno. Mismo esquema de URL que el icono. |
| GET | `/v1/servers/{serverId}/members` | si (miembro) | Listar los miembros del server (paginado). |
| POST | `/v1/servers/{serverId}/invites` | si (`CREATE_INVITE`) | Generar invitación (JSON opcional: `max_uses?`; expira a los 7 días). |
| GET | `/v1/servers/{serverId}/invites` | si (`CREATE_INVITE`) | Listar todas las invitaciones del server (activas, revocadas o vencidas), más nueva primero. Sin filtro por estado del lado del back. |
| DELETE | `/v1/invites/{code}` | si (admin o quien la creó) | Revocar una invitación (idempotente). |
| POST | `/v1/invites/{code}/join` | si | Unirse a un servidor con una invitación. 403 si el usuario está baneado. Recibe el rol por defecto del server. |
| DELETE | `/v1/servers/{serverId}/members/{userId}` | si (uno mismo) | Abandonar un servidor. `userId` debe ser el propio caller; el owner no puede abandonar así (409, primero tiene que transferir la propiedad o eliminar el servidor). |
| POST | `/v1/servers/{serverId}/ownership-transfers` | si (owner) | Ofrecer la propiedad a otro miembro (`to_user_id`). Crea una transferencia pendiente; no cambia nada hasta que el destinatario responde. Rechaza si `to_user_id` no es miembro (400) o si ya hay una transferencia pendiente para ese server (409). |
| GET | `/v1/servers/{serverId}/ownership-transfers/pending` | si (miembro) | Ver la transferencia pendiente del server, si hay una. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/accept` | si (destinatario) | Aceptar: pasa a ser el nuevo owner y el anterior queda como miembro regular. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/reject` | si (destinatario) | Rechazar: la propiedad no cambia. |
| POST | `/v1/servers/{serverId}/ownership-transfers/{transferId}/cancel` | si (owner que la inicio) | Cancelar la transferencia pendiente antes de que el destinatario responda (p. ej. para ofrecersela a otro miembro). |
| POST | `/v1/servers/{serverId}/roles` | si (admin) | Crear rol (`name`, `color`). Se crea al final de la jerarquía, con `VIEW_CHANNELS` y `SEND_MESSAGES`. |
| GET | `/v1/servers/{serverId}/roles` | si | Listar roles de un server, ordenados por jerarquía (`position` 1 = el más alto). |
| PATCH | `/v1/servers/{serverId}/roles/reorder` | si (admin) | Reordenar la jerarquía (`role_ids`: todos los roles del server, del más alto al más bajo). 400 si la lista no coincide exactamente; 403 si un admin no-owner mueve un rol que no está por debajo de su rol más alto. |
| GET | `/v1/roles/{roleId}` | si | Un rol puntual (mismo shape que un elemento de `GET /v1/servers/{serverId}/roles`). 404 si no existe. |
| PATCH | `/v1/roles/{roleId}` | si (admin) | Editar permisos/nombre/color de un rol. |
| POST | `/v1/servers/{serverId}/members/{userId}/roles` | si (admin) | Asignar un rol a un miembro (idempotente). |
| DELETE | `/v1/servers/{serverId}/members/{userId}/roles/{roleId}` | si (admin) | Quitar un rol de un miembro. 404 si no lo tiene. |
| GET | `/v1/servers/{serverId}/members/{userId}/roles` | si (miembro) | Listar los roles de un miembro. 404 si `userId` no es miembro. |
| GET | `/v1/servers/{serverId}/members/{userId}/permissions` | si (miembro) | Permisos efectivos del miembro: todos si es el dueño o tiene `ADMINISTRATOR`, si no la unión de los de sus roles. 404 si `userId` no es miembro. |
| PUT | `/v1/servers/{serverId}/default-role` | si (admin) | Definir el rol por defecto del server. |
| DELETE | `/v1/roles/{roleId}` | si (admin) | Eliminar un rol (optativa). Limpia asignaciones; rechaza con 409 si el rol es el default actual del server (hay que asignar otro default primero). |
| POST | `/v1/servers/{serverId}/channels` | si (admin) | Crear canal de texto o voz (`name`, `kind`, `category_id?`). 400 si el nombre esta vacio o ya existe otro canal con ese nombre en la misma categoria (o en el "sin categoria", segun corresponda). |
| PATCH | `/v1/channels/{channelId}` | si (admin) | Editar nombre y/o topic de un canal. |
| DELETE | `/v1/channels/{channelId}` | si (admin) | Eliminar un canal. |
| POST | `/v1/servers/{serverId}/categories` | si (admin) | Crear categoria (`name`). |
| PATCH | `/v1/categories/{categoryId}` | si (admin) | Renombrar una categoria (`name`). |
| PATCH | `/v1/channels/{channelId}/category` | si (admin) | Mover un canal a una categoria (`category_id`), o sacarlo a "sin categoria" (`category_id: null`). |
| PATCH | `/v1/servers/{serverId}/channels/reorder` | si (admin) | Reordenar los canales de una categoria (`category_id?`, `channel_ids`: el orden completo y exacto de esa categoria). 400 si la lista no coincide exactamente con los canales actuales de esa categoria. |
| PATCH | `/v1/servers/{serverId}/categories/reorder` | si (admin) | Reordenar las categorias del server (`category_ids`: el orden completo y exacto). 400 si la lista no coincide exactamente. |
| POST | `/v1/servers/{serverId}/bans` | si (`BAN_MEMBERS`) | Banear a un miembro de rango menor (`user_id`, `reason?`). 201 con el baneo; 403 si es el dueño o tiene rango igual o mayor; 404 si no es miembro; 409 si ya está baneado. |
| GET | `/v1/servers/{serverId}/bans` | si (`BAN_MEMBERS`) | Listar los baneos del server (paginado: `limit`, `offset`; más nuevo primero). |
| DELETE | `/v1/servers/{serverId}/bans/{userId}` | si (`BAN_MEMBERS`) | Revocar un baneo. 204; 404 si el usuario no está baneado. No lo vuelve miembro: tiene que volver a unirse con una invitación. |
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
