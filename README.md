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
- **Menciones** (#46, optativa)

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
| RABBITMQ_URL | - (vacio) | Conexion al broker para publicar eventos de dominio y consumir `users.profile_updated` (exchange `discordia.events`, ver ADR-07). Vacia = publisher en no-op y sin réplica de perfiles (`profile: null` en `GET /members`); el servicio funciona igual sin RabbitMQ. |

Publicar un evento nunca pasa directo por RabbitMQ desde el request: el
handler escribe una fila en `outbox_events` en la misma transacción que el
cambio de negocio (crear server, sacar un miembro, etc.), y un poller
(`internal/outbox`, arrancado en `main.go`, cada `outbox.PollInterval`) es
el único que efectivamente publica -- así un crash o una caída de RabbitMQ
entre el commit y el publish nunca pierde el evento silenciosamente, solo
lo retrasa. El mensaje además se publica con `DeliveryMode: amqp.Persistent`
(`internal/events/publisher.go`), para que RabbitMQ tampoco lo pierda si el
broker se reinicia con el mensaje todavía sin consumir.

### Réplica de perfiles de usuario

`GET /v1/servers/{serverId}/members` devuelve el perfil público de cada
miembro (`name`, `avatar_url`, `description`, `status_text`, `status_emoji`)
sin llamar a identify:

- identify publica `users.profile_updated` (foto completa del perfil + `version`
  creciente) al registrarse un usuario y en cada cambio de perfil o estado.
- `internal/events/profile_consumer` consume la cola durable
  `servers.user_profiles` (ack manual, reconexión con backoff de 1 s a 30 s,
  conexión `discordia-servers-user-profiles`) y guarda la foto en la tabla
  `user_profiles`.
- El upsert solo aplica una `version` mayor a la guardada: eventos repetidos o
  que llegan en desorden no pisan un perfil más nuevo. Un mensaje inválido
  (JSON roto, `user_id` que no es UUID, `version` < 1) se descarta con log; un
  error de base se reintenta.
- `GET /members` hace `LEFT JOIN user_profiles`: `profile` es `null` si todavía
  no llegó el evento de ese usuario (el front puede caer a `GET /v1/users/{id}`).
- Es consistencia eventual: un cambio de perfil tarda unos segundos en verse
  (el tick del poller de identify más la entrega).
- servers declara la cola al arrancar, así que tiene que estar desplegado antes
  de que identify empiece a publicar; si no, esos eventos se pierden y hay que
  relanzar el backfill de identify.
- Suma una conexión a RabbitMQ (ojo con el límite de 20 del free tier de
  CloudAMQP).

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
│   ├── events/                 publisher de eventos hacia RabbitMQ (exchange discordia.events) y profile_consumer (réplica de perfiles)
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
`MANAGE_SERVER`, `ADMINISTRATOR`, `CREATE_INVITE`, `MANAGE_MESSAGES`,
`MENTION_EVERYONE`, `ADD_REACTIONS` y `CONNECT`. La API siempre usa los nombres; la máscara de bits es
interna.

- **Administradores.** Son el dueño del servidor y los miembros con algún rol
  que tenga `ADMINISTRATOR`. Tienen todos los permisos de forma implícita. Lo
  que es solo del dueño (transferir la propiedad, no poder
  abandonar el servidor) sigue siendo solo del dueño. Un administrador no-owner
  no puede sacarse `ADMINISTRATOR` a sí mismo (ni editando, ni borrando, ni
  desasignando el rol que se lo da).
- **Permisos de gestión.** Gestionar roles (crear, editar, borrar, asignar,
  quitar, reordenar, rol por defecto) pide `MANAGE_ROLES`; canales y
  categorías, `MANAGE_CHANNELS`; la configuración del servidor,
  `MANAGE_SERVER`. Los administradores los tienen implícitos.
- **Moderar mensajes.** `MANAGE_MESSAGES` permite eliminar mensajes de otros
  miembros (lo aplica el servicio `messaging`, que tiene que consultar los
  permisos efectivos **del canal**, ver "Permisos por canal").
  No viene en el conjunto por defecto de un rol nuevo: se asigna con
  `PATCH /v1/roles/{roleId}`. El autor de un mensaje puede eliminar el suyo sin
  este permiso.
- **Mencionar a todos.** `MENTION_EVERYONE` permite mencionar `@everyone` y
  cualquier rol en un mensaje (historia #46, CA2/CA3). Sin él esas menciones
  quedan como texto plano: el mensaje se envía igual, pero no notifica a nadie
  (CA4). Mencionar a un usuario puntual no pide ningún permiso (CA1). Como
  `MANAGE_MESSAGES`, no viene en el conjunto por defecto de un rol nuevo y los
  administradores lo tienen implícito. Ver [Menciones](#menciones).
- **Reaccionar.** `ADD_REACTIONS` permite reaccionar a mensajes con emojis
  (historia #48, lo aplica `messaging`). Viene en el conjunto por defecto de un
  rol nuevo y se puede sobrescribir por canal. La migración 0023 se lo dio a
  todo rol que ya tenía `SEND_MESSAGES` (y lo copió en los overrides de
  `SEND_MESSAGES`), así nadie perdió la posibilidad de reaccionar.
- **Conectarse a voz.** `CONNECT` permite unirse a canales de voz (historia
  #17, CA2): `GET /v1/channels/{channelId}/voice-access` pide ver el canal y
  tener `CONNECT` en él, con los overrides aplicados. Quien lo ve pero no lo
  tiene recibe `403` con `reason: missing_permission` y
  `permission: CONNECT`. Viene en el conjunto por defecto de un rol nuevo (y
  en `@everyone`) y se puede sobrescribir por canal, para dejar a un rol
  afuera de un solo canal de voz. La migración 0025 se lo dio a todo rol que
  ya tenía `VIEW_CHANNELS` (y lo copió en los overrides que permiten
  `VIEW_CHANNELS`), así nadie perdió el acceso a la voz.
- **No se puede dar lo que no se tiene.** Quien no es administrador no puede
  agregarle a un rol un permiso que él mismo no tiene, ni asignar un rol que
  traiga alguno (`403` con `reason: permission_not_held`). Si no, alguien con
  `MANAGE_ROLES` podría darle `ADMINISTRATOR` a un rol por debajo del suyo y
  asignárselo. Los permisos que el rol ya tenía se pueden conservar o quitar
  igual, y quitar un rol nunca está restringido por esto.
- **Permisos de un rol nuevo.** Todo rol se crea con `VIEW_CHANNELS`,
  `SEND_MESSAGES`, `ADD_REACTIONS` y `CONNECT` (`role.DefaultPermissions`); después se editan con
  `PATCH /v1/roles/{roleId}`. Los roles creados antes de este default no se
  tocaron.
- **A nivel servidor, los permisos solo suman.** Los permisos de un miembro
  en el servidor son la **unión** de los de todos sus roles (ningún rol
  "niega" un permiso de otro). Por eso, quitarle un rol no le saca un permiso
  que también le da otro rol, y los cambios en un rol se aplican de inmediato
  a todos los que lo tienen.
  `GET /v1/servers/{serverId}/members/{userId}/permissions` devuelve los
  permisos **efectivos en el servidor**, con la misma regla que usa el
  servicio para autorizar: todos para el dueño y los administradores, la
  unión de sus roles para el resto. El cliente puede decidir qué mostrar con
  un solo "incluye X". Dentro de un canal, en cambio, un override puede quitar
  permisos (ver "Permisos por canal").
- **Jerarquía.** Cada rol tiene una `position` (1 = el más alto); los roles
  nuevos se crean al final. El rango de un miembro es su rol más alto, y el
  dueño está por encima de todos. Como los permisos solo suman, la jerarquía
  **no resuelve conflictos de permisos** (no los hay): define el **rango**, y
  se usa para:
  - **Reordenar roles** (`PATCH /v1/servers/{serverId}/roles/reorder`, con la
    lista completa de roles, del más alto al más bajo): quien no es el dueño
    solo puede mover los roles que están por debajo de su rol más alto.
  - **Banear y expulsar**: solo se puede banear o expulsar a alguien de rango
    estrictamente menor.
  - **Gestionar roles**: quien no es el dueño solo puede editar, borrar,
    asignar, quitar o poner como rol por defecto los roles que están
    **estrictamente por debajo** de su rol más alto (`403` con
    `reason: role_above_own`). Así no puede darse (ni sacarle a otro) un rol
    que lo supere para después banear o reordenar por encima de su rango.
    Tampoco puede editar su propio rol más alto. El dueño puede todo. La misma
    regla vale para los overrides de un rol en un canal, salvo `@everyone`.

## Permisos por canal
Un **override** es una excepción de permisos para un rol en un solo canal:
qué permisos gana el rol ahí (`allow`) y cuáles pierde (`deny`), sin tocar sus
permisos en el resto del servidor (HU #57). Se guardan en la tabla
`channel_permission_overrides` (migración 0020), uno por rol y canal.

- **Qué se puede sobrescribir.** Solo los permisos que tienen sentido en un
  canal: `VIEW_CHANNELS`, `SEND_MESSAGES`, `MANAGE_MESSAGES`, `ADD_REACTIONS` y
  `CONNECT` (`role.ChannelScopedPermissions`). Cualquier otro da `400`
  `permission_not_overridable`; el mismo permiso en `allow` y `deny`, `400`
  `permission_conflict`. Con las dos listas vacías, el override se borra.
- **Quién.** Hace falta `MANAGE_ROLES`, y el rol tiene que estar por debajo
  del propio (`role_above_own`). Excepción: el override de `@everyone` lo
  puede tocar cualquiera con `MANAGE_ROLES`, porque es el caso principal (un
  canal de solo lectura o privado para todos). Solo se pueden permitir
  (`allow`) permisos que uno mismo tiene (`permission_not_held`); denegar no
  tiene restricción.
- **Cómo se calcula.** Los permisos de un miembro en un canal
  (`channelperm.Effective`) son: todos para el dueño y los administradores
  (los overrides no los afectan); para el resto, la unión de sus roles, a la
  que se le aplica primero el override de `@everyone` (se sacan sus `deny` y
  se suman sus `allow`) y después, todos juntos, los de sus otros roles (unión
  de `deny`, unión de `allow`; si un rol permite y otro deniega, gana el
  permiso). Ejemplos:
  - `#anuncios` de solo lectura: `@everyone` con `deny: [SEND_MESSAGES]`, y
    `Moderador` con `allow: [SEND_MESSAGES]` para que ellos sí publiquen.
  - `#staff` privado: `@everyone` con `deny: [VIEW_CHANNELS]` y `Moderador` con
    `allow: [VIEW_CHANNELS]`.
- **Permisos efectivos de un canal.**
  `GET /v1/channels/{channelId}/members/{userId}/permissions` devuelve los
  permisos del miembro en ese canal, con la misma forma y los mismos códigos
  que el endpoint por servidor. Es el que tienen que usar los servicios que
  autorizan por canal (`messaging` para leer, escribir y moderar; voz).
- **Canales ocultos.** Quien no tiene `VIEW_CHANNELS` en un canal no lo ve:
  - `GET /v1/servers` y `GET /v1/servers/{serverId}` no lo incluyen en
    `channels` (las categorías se devuelven siempre, aunque queden vacías).
    Quien no es miembro recibe `GET /v1/servers/{serverId}` con
    `channels: []`.
  - Editarlo, borrarlo o moverlo de categoría da `404`, aunque se tenga
    `MANAGE_CHANNELS`, para no revelar que existe.
  - Si es de voz, `GET /v1/channels/{channelId}/voice-access` da `404`: no
    se puede entrar a la llamada. Al revés, un override que le da
    `VIEW_CHANNELS` a un rol que no lo tiene en el servidor alcanza para
    entrar. Quien no es miembro sigue recibiendo `403`.
  - Al reordenar una categoría se mandan solo los canales visibles: los
    ocultos conservan su lugar y los visibles ocupan los lugares restantes en
    el orden pedido. Mandar un canal oculto da `400 reorder_invalid`.
  - Los canales empatados en `position` (los recién creados quedan en `0`
    hasta el primer reorder) se ordenan por `id`, igual en los listados y en el
    reorder.
- **Perder acceso en vivo.** Cuando un cambio le saca `VIEW_CHANNELS` a
  alguien en un canal, se publica `servers.channel_access_revoked`
  (`server_id`, `channel_id`, `user_ids`): uno por canal afectado, solo con
  quienes **perdieron** el acceso (ganarlo no publica nada; el cliente lo ve al
  refrescar la lista de canales). Lo disparan crear, editar o borrar un
  override; editar o borrar un rol; asignar o quitar un rol; aceptar una
  transferencia de propiedad (el dueño anterior deja de verlo todo); y
  expulsar, banear o abandonar el servidor (el miembro aparece en un evento por
  cada canal que veía, además del `servers.member_left`). Se calcula quién ve
  cada canal antes y después de la escritura, dentro de la misma transacción,
  y se encola en el outbox, así que se publica solo si el cambio se aplica.
  `messaging` cierra el socket de esos usuarios en ese canal y `voice-sfu` los
  saca de la sala de voz; al reconectar, el `join` y `voice-access` vuelven a
  chequear el estado actual.

## Eliminar un servidor
`DELETE /v1/servers/{serverId}` con cuerpo `{"confirm_name": "<nombre exacto del servidor>"}`
(la UI lo resuelve pidiendo que se reingrese el nombre):
- **Solo el owner.** Cualquier otro (miembro, administrador o con `MANAGE_SERVER`)
  recibe `403`, y eso se decide **antes** de mirar la confirmación, así que no
  sirve para averiguar el nombre.
- **Confirmación obligatoria.** Sin cuerpo, vacío, con otro nombre o con otra
  capitalización: `400` (`confirm_name` / `confirmation_mismatch`) y no se borra
  nada. Se ignoran los espacios de los extremos. JSON roto: `400 invalid_json`.
- **Todo o nada.** En una sola transacción se borra el servidor (las cascadas de la
  base se llevan canales, categorías, roles y sus asignaciones, miembros,
  invitaciones, bans y transferencias de propiedad) y se encolan en el outbox un
  `servers.member_left` por cada miembro y un `servers.server_deleted` con los ids
  de todos los canales. Si algo falla, no se borra nada. La fila del servidor se
  bloquea mientras tanto, así que dos borrados simultáneos tienen un único ganador
  y el otro recibe `404`.
- **Fuera de la base.** El ícono y el banner se borran del almacenamiento después
  del commit, sin que un fallo ahí deshaga el borrado. `messaging` borra los
  mensajes al consumir `servers.server_deleted`, y `identify-service` actualiza
  sus "servidores en común" con los `member_left` de siempre.
- Respuesta `204` sin cuerpo; el mismo pedido otra vez da `404`. No es reversible.

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
- **Expulsar** (`POST /v1/servers/{serverId}/kicks`, `user_id`): requiere
  `KICK_MEMBERS` (los administradores lo tienen implícito) y que el objetivo
  sea un miembro de rango menor; nunca el dueño ni uno mismo (para irse está
  `DELETE /v1/servers/{serverId}/members/{userId}`). En una sola transacción
  se saca al miembro (con sus roles), se cancela una transferencia de
  propiedad pendiente hacia él y se encolan `servers.member_left` y
  `servers.member_kicked`. A diferencia del baneo, no queda nada registrado:
  puede volver a unirse con cualquier invitación válida (HU #59).
- **Palabras prohibidas** (`GET` y `PUT /v1/servers/{serverId}/banned-words`,
  `{"words": [...]}`): requiere `MANAGE_MESSAGES` a nivel servidor (sin mirar
  overrides de canal, porque la lista es de todo el servidor; los
  administradores lo tienen implícito). El `PUT` manda la lista **entera** y
  reemplaza la anterior; vacía, saca el filtro. Cada entrada es una palabra o
  frase de hasta 50 caracteres, hasta 100 entradas; se guardan en minúsculas,
  sin espacios de más y sin repetidas. Cada cambio encola
  `servers.word_filter_updated` con la lista completa, y `messaging` rechaza
  desde ese momento los mensajes que contengan alguna (sin distinguir
  mayúsculas ni tildes, solo palabras completas). Nadie queda exento, y los
  mensajes ya enviados no se tocan (HU de filtro automático).

Eventos que publica el servicio (exchange `discordia.events`, contratos en
`github.com/discordia-grupo01/events-contract` y AsyncAPI en `discordia-docs`):
`servers.member_joined`, `servers.member_left`, `servers.member_banned`,
`servers.member_unbanned`, `servers.member_kicked`, `servers.channel_created`,
`servers.channel_deleted`, `servers.channel_access_revoked`,
`servers.server_deleted`, `servers.member_muted`, `servers.member_unmuted` y
`servers.word_filter_updated`,
más cinco **fotos completas** con las que `messaging` arma su réplica de
permisos y decide sin llamar acá (ADR 0003):

| Evento | Se publica cuando |
|---|---|
| `servers.owner_updated` | se crea el servidor y se acepta una transferencia de propiedad |
| `servers.role_updated` | se crea un rol o cambian sus permisos, nombre o color (viajan los permisos por nombre y `is_everyone`) |
| `servers.role_deleted` | se borra un rol (el consumidor lo saca de miembros y overrides) |
| `servers.member_roles_updated` | alguien se une (creando el servidor o por invitación) o se le asigna o quita un rol: todos sus roles, `@everyone` incluido |
| `servers.channel_overrides_updated` | se crea, edita o borra un override: todos los overrides del canal (lista vacía si no queda ninguno) |

Cada foto se escribe en el outbox en la **misma transacción** que el cambio
(`repository/access_events.go`) y lleva el estado ya escrito, así que un
consumidor puede descartar la que llegue más vieja que la que tiene. La
migración `0022_backfill_access_events` re-publica todo lo existente: hay que
correrla con la cola del consumidor ya creada, o repetirla después.

## Menciones
Los mensajes viven en `messaging`, pero quién puede mencionar a quién depende
de los miembros, roles y permisos, que viven acá. `messaging` ya no llama a este
endpoint (resuelve las menciones con su réplica local, con las mismas reglas
que se describen abajo); `POST /v1/servers/{serverId}/mentions/resolve` sigue
disponible y recibe el canal y lo que se encontró en el texto:

```json
{ "channel_id": "...", "user_ids": ["..."], "role_ids": ["..."], "everyone": true }
```

y recibe qué menciones valen y a quién notificar:

```json
{ "user_ids": ["..."], "role_ids": ["..."], "everyone": true, "recipient_ids": ["..."] }
```

- `user_ids`: solo los mencionados que son miembros del servidor.
- `role_ids` y `everyone`: solo si el autor tiene `MENTION_EVERYONE` (si no,
  vuelven vacíos/`false` y el mensaje se envía con eso como texto plano).
  El id del rol `@everyone` nunca cuenta como mención de rol: para eso está
  `everyone`.
- `recipient_ids`: la unión de todo lo anterior, filtrada a quienes pueden ver
  el canal (`VIEW_CHANNELS`, la misma regla que `Standing.Has`: el dueño y los
  administradores siempre), y **sin el autor**. Con `everyone`, son todos los
  miembros que ven el canal.
- El autor tiene que ser miembro (`403` si no). Si puede escribir en el canal
  lo sigue chequeando `messaging` con `SEND_MESSAGES`.
- Los repetidos cuentan una vez. Máximo 50 usuarios y 20 roles distintos por
  mensaje (`400` con `reason: too_many`).
- `channel_id` tiene que ser un canal de ese servidor (`404` si no existe,
  `400` si es de otro). Hoy `VIEW_CHANNELS` es a nivel servidor, así que el
  canal no cambia quién lo ve; se pide igual para que el día que haya permisos
  por canal solo cambie la query.

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
| GET | `/v1/servers` | si | Mis servidores (de los que soy owner o miembro), con sus categorias y los canales que puedo ver (sin los ocultos por un override). |
| GET | `/v1/servers/{serverId}` | si | Un servidor puntual (nombre, canales, categorias, icon_url, banner_url). Ej.: después de un `join`, para obtener los datos con solo el `server_id`. Cada canal trae su `category_id` (`null` si esta "sin categoria"). Solo trae los canales que el caller puede ver; si no es miembro, `channels: []`. |
| PATCH | `/v1/servers/{serverId}` | si (admin) | Editar nombre, ícono y/o banner (multipart: `name?`, `icon?`, `banner?`, `remove_banner?`). Todos los campos son opcionales; sin permisos da 403, datos invalidos dan 400 sin persistir nada. |
| DELETE | `/v1/servers/{serverId}` | si (owner) | Eliminar el servidor y todo lo suyo (cuerpo JSON `confirm_name` con el nombre exacto). 204; 400 si falta o no coincide la confirmación; 403 si no es el owner; 404 si no existe. Irreversible. |
| GET | `/v1/servers/{serverId}/icon` | no (público) | Sirve el archivo del ícono (subido o el default generado). En produccion (Supabase) `icon_url` apunta directo al bucket público; esta ruta queda como fallback para setups sin Supabase. |
| GET | `/v1/servers/{serverId}/banner` | no (público) | Sirve el archivo del banner. 404 si el server no tiene uno. Mismo esquema de URL que el icono. |
| GET | `/v1/servers/{serverId}/members` | si (miembro) | Listar los miembros del server (paginado), cada uno con su `profile` (`null` si todavía no se replicó) y `muted_until` (hasta cuándo está silenciado, `null` si no lo está). |
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
| GET | `/v1/servers/{serverId}/roles` | si (miembro) | Listar roles de un server, ordenados por jerarquía (`position` 1 = el más alto). |
| PATCH | `/v1/servers/{serverId}/roles/reorder` | si (admin) | Reordenar la jerarquía (`role_ids`: todos los roles del server, del más alto al más bajo). 400 si la lista no coincide exactamente; 403 si un admin no-owner mueve un rol que no está por debajo de su rol más alto. |
| GET | `/v1/roles/{roleId}` | si (miembro) | Un rol puntual (mismo shape que un elemento de `GET /v1/servers/{serverId}/roles`). 404 si no existe; 403 si quien consulta no es miembro del server del rol. |
| PATCH | `/v1/roles/{roleId}` | si (admin) | Editar permisos/nombre/color de un rol. |
| POST | `/v1/servers/{serverId}/members/{userId}/roles` | si (admin) | Asignar un rol a un miembro (idempotente). |
| DELETE | `/v1/servers/{serverId}/members/{userId}/roles/{roleId}` | si (admin) | Quitar un rol de un miembro. 404 si no lo tiene. |
| GET | `/v1/servers/{serverId}/members/{userId}/roles` | si (miembro) | Listar los roles de un miembro. 404 si `userId` no es miembro. |
| GET | `/v1/servers/{serverId}/members/{userId}/permissions` | si (miembro) | Permisos efectivos del miembro: todos si es el dueño o tiene `ADMINISTRATOR`, si no la unión de los de sus roles. 404 si `userId` no es miembro. |
| PUT | `/v1/servers/{serverId}/default-role` | si (admin) | Definir el rol por defecto del server. |
| DELETE | `/v1/roles/{roleId}` | si (admin) | Eliminar un rol (optativa). Limpia asignaciones; rechaza con 409 si el rol es el default actual del server (hay que asignar otro default primero). |
| POST | `/v1/servers/{serverId}/channels` | si (admin) | Crear canal de texto o voz (`name`, `kind`, `category_id?`). 400 si el nombre esta vacio o ya existe otro canal con ese nombre en la misma categoria (o en el "sin categoria", segun corresponda). |
| PATCH | `/v1/channels/{channelId}` | si (`MANAGE_CHANNELS`) | Editar nombre y/o topic de un canal. 404 si el caller no lo ve. |
| DELETE | `/v1/channels/{channelId}` | si (`MANAGE_CHANNELS`) | Eliminar un canal. 404 si el caller no lo ve. |
| POST | `/v1/servers/{serverId}/categories` | si (admin) | Crear categoria (`name`). |
| PATCH | `/v1/categories/{categoryId}` | si (admin) | Renombrar una categoria (`name`). |
| PATCH | `/v1/channels/{channelId}/category` | si (`MANAGE_CHANNELS`) | Mover un canal a una categoria (`category_id`), o sacarlo a "sin categoria" (`category_id: null`). 404 si el caller no lo ve. |
| PATCH | `/v1/servers/{serverId}/channels/reorder` | si (`MANAGE_CHANNELS`) | Reordenar los canales de una categoria (`category_id?`, `channel_ids`: el orden completo y exacto de los canales de esa categoria que el caller ve). Los ocultos conservan su lugar. 400 si la lista no coincide exactamente. |
| PATCH | `/v1/servers/{serverId}/categories/reorder` | si (admin) | Reordenar las categorias del server (`category_ids`: el orden completo y exacto). 400 si la lista no coincide exactamente. |
| POST | `/v1/servers/{serverId}/bans` | si (`BAN_MEMBERS`) | Banear a un miembro de rango menor (`user_id`, `reason?`). 201 con el baneo; 403 si es el dueño o tiene rango igual o mayor; 404 si no es miembro; 409 si ya está baneado. |
| GET | `/v1/servers/{serverId}/bans` | si (`BAN_MEMBERS`) | Listar los baneos del server (paginado: `limit`, `offset`; más nuevo primero). |
| DELETE | `/v1/servers/{serverId}/bans/{userId}` | si (`BAN_MEMBERS`) | Revocar un baneo. 204; 404 si el usuario no está baneado. No lo vuelve miembro: tiene que volver a unirse con una invitación. |
| POST | `/v1/servers/{serverId}/kicks` | si (`KICK_MEMBERS`) | Expulsar a un miembro de rango menor (`user_id`). 204; 400 si es uno mismo; 403 si es el dueño o tiene rango igual o mayor; 404 si no es miembro. Puede volver con una invitación. |
| GET | `/v1/servers/{serverId}/banned-words` | si (`MANAGE_MESSAGES`) | Lista de palabras prohibidas del server, en el orden en que se guardaron. |
| PUT | `/v1/servers/{serverId}/banned-words` | si (`MANAGE_MESSAGES`) | Reemplazar la lista entera (`words`: hasta 100, de hasta 50 caracteres; vacía saca el filtro). 200 con la lista como quedó guardada; 400 si alguna está vacía, es muy larga o son más de 100. Ver "Moderación". |
| PUT | `/v1/channels/{channelId}/permissions/roles/{roleId}` | si (`MANAGE_ROLES`) | Crear o reemplazar el override de un rol en un canal (`allow?`, `deny?`: `VIEW_CHANNELS`, `SEND_MESSAGES`, `MANAGE_MESSAGES`, `ADD_REACTIONS`, `CONNECT`). 200 con el override; 204 si las dos listas vienen vacías (se borra). 400 si el permiso no se puede sobrescribir o está en las dos listas; 403 si el rol no está por debajo del propio (salvo `@everyone`) o se permite algo que el caller no tiene. |
| DELETE | `/v1/channels/{channelId}/permissions/roles/{roleId}` | si (`MANAGE_ROLES`) | Quitar el override de un rol en un canal. 204; 404 `channel_override_not_found` si no había. |
| GET | `/v1/channels/{channelId}/permissions` | si (`MANAGE_ROLES`) | Listar los overrides del canal (`@everyone` primero, después por jerarquía). |
| GET | `/v1/channels/{channelId}/members/{userId}/permissions` | si (miembro) | Permisos efectivos del miembro en ese canal (con los overrides aplicados). Misma forma y códigos que el endpoint por servidor. |
| POST | `/v1/servers/{serverId}/mentions/resolve` | si (miembro) | Para `messaging`: valida las menciones de un mensaje (`channel_id`, `user_ids`, `role_ids`, `everyone`) y devuelve cuáles valen y a quién notificar (`recipient_ids`). Roles y `@everyone` solo cuentan con `MENTION_EVERYONE`. Ver "Menciones". |
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
