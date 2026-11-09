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
| DATABASE_MAX_CONNS | 0 (el default de pgx) | Maximo de conexiones del pool. 0 deja el default de pgx, `max(4, CPUs)`, que se queda corto cuando varios clientes consultan a la vez y las consultas esperan una conexion libre. Antes de subirlo, mirar el limite de conexiones de la base (o del pooler de Supabase): cada instancia abre hasta ese numero. |
| USER_ID_HEADER | - (vacio) | Vacio = el usuario sale del `sub` del JWT (`Authorization: Bearer ...`), que es lo que llega via Kong. Solo para desarrollo local (curl/tests): con `X-User-Id` el servicio toma ese header como identidad del usuario. **Nunca setearlo en un entorno desplegado**: Kong no filtra ese header, asi que cualquier cliente podria hacerse pasar por otro usuario. |
| ICON_DIR | ./data/icons | Directorio donde se guardan los iconos subidos (si no se usa Supabase Storage, ver debajo). |
| ICON_MAX_BYTES | 5242880 | Tamano maximo (bytes) de un icono o banner. |
| SUPABASE_STORAGE_URL | - (vacio) | URL del proyecto de Supabase (`https://xxxx.supabase.co`). Si esta + `SUPABASE_STORAGE_BUCKET` + `SUPABASE_SERVICE_KEY`, los iconos van a ese bucket en vez de a `ICON_DIR` (ver seccion "Supabase Storage" mas abajo). |
| SUPABASE_STORAGE_BUCKET | - (vacio) | Nombre del bucket de Storage. |
| SUPABASE_SERVICE_KEY | - (vacio) | `service_role` key del proyecto (Project Settings > API) -- nunca la `anon` key, esta se salta las políticas RLS del bucket. |
| RABBITMQ_URL | - (vacio) | Conexion al broker para publicar eventos de dominio y consumir `users.profile_updated` (exchange `discordia.events`, ver ADR-07). Vacia = publisher en no-op y sin réplica de perfiles (`profile: null` en `GET /members`); el servicio funciona igual sin RabbitMQ. |
| ADMIN_EVENTS_HMAC_SECRET | - (vacio) | Secreto compartido con `discordia-admin` para verificar la firma HMAC de `admin.user_suspended` / `admin.user_reactivated` y de `admin.server_suspended` / `admin.server_reactivated` / `admin.server_deletion_requested`. **Obligatoria si `RABBITMQ_URL` está seteada** (si falta, el servicio no arranca); sin RabbitMQ no se aplican suspensiones. Los eventos sin firma o con firma inválida se descartan con un `warn` y no cambian ningún estado. |
| BILLING_EVENTS_HMAC_SECRET | - (vacio) | Secreto compartido con `billing` para firmar y verificar los eventos de la saga de boosts, con el mismo valor en las dos VMs. **Obligatoria si `RABBITMQ_URL` está seteada** (si falta, el servicio no arranca). Los eventos con firma inválida se descartan con un `warn`. |

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
- **Por pedido del staff de la plataforma** (HU #68): el backoffice no llama a este
  endpoint; `discordia-admin` publica `admin.server_deletion_requested` y este
  servicio lo ejecuta con `DeleteServerByStaff`, que hace exactamente lo mismo en
  la misma transacción y publica los mismos eventos, sin exigir ser el owner ni el
  nombre (esa confirmación ya la pidió admin). Ver "Servidores suspendidos o
  eliminados por la plataforma".

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

- **Boosts** (`GET /v1/servers/{serverId}/boosts`, para cualquier miembro): boosts
  activos, nivel, beneficios y cuánto falta para el siguiente nivel. Los puntos los
  cobra `billing`; este servicio decide si acepta el boost, en la cola
  `servers.billing_events`. Un servidor suspendido o quien no es miembro reciben un
  rechazo, y `billing` devuelve los puntos; una falla técnica reencola el evento.
  Cada boost se guarda por `boost_id`, así un evento repetido recibe la misma
  respuesta. Niveles: 1 con 5 boosts (+20 emojis, ícono GIF), 2 con 15 (banner,
  adjuntos de 25 MB) y 3 con 30 (vanity, insignia). Si un servidor baja de nivel, los
  emojis que sobran siguen funcionando pero no se suman nuevos, y el ícono GIF, el
  banner y la vanity se guardan pero dejan de mostrarse.
- **Vanity** (`PATCH /v1/servers/{serverId}/vanity`, pide `MANAGE_SERVER`): el código
  se usa en `POST /v1/invites/{code}/join`. Ponerlo pide nivel 3; sacarlo, con
  `{"code": null}`, no pide nivel.
- **Emojis personalizados** (`POST`, `GET` y `DELETE` en
  `/v1/servers/{serverId}/emojis`): crear y borrar pide `MANAGE_SERVER` (el
  dueño y los administradores lo tienen implícito); listar, ser miembro. El
  alta es multipart con `name` (2 a 32 letras, números o guiones bajos, único en
  el servidor sin distinguir mayúsculas) e `image` (PNG, WebP o GIF, hasta 5 MB,
  detectado por el contenido del archivo y no por su nombre). Un GIF crea un
  emoji animado; usarlo en mensajes requiere Premium, y eso lo controla
  `messaging`. Cada servidor tiene 50 lugares (70 desde el nivel 1 de boosts), y `max_emojis` en el listado
  indica cuántos son. Borrar es lógico: el emoji deja de listarse y libera su
  lugar y su nombre, pero la fila y la imagen se conservan para que los mensajes
  y reacciones que ya lo usan lo sigan mostrando. Cada alta y cada baja encolan
  `servers.emoji_created` o `servers.emoji_deleted` en la misma transacción. Las
  imágenes van al mismo bucket que los íconos, bajo `emojis/<serverId>/`, así que
  el bucket tiene que aceptar `image/gif`. En desarrollo se guardan en
  `ICON_DIR` y se sirven en `GET /v1/servers/{serverId}/emojis/{key}/image`; en
  producción esa ruta no existe porque el bucket es público. Al borrar un
  servidor se borran sus filas y todas las imágenes de su carpeta, también las de
  emojis ya borrados. 

- **Reportes** (`POST /v1/servers/{serverId}/reports`): cualquier miembro
  puede reportar un mensaje (`kind: "message"`, `channel_id` y `message_id`) o
  a otro miembro (`kind: "user"`, `user_id`), siempre con un `reason` de hasta
  512 caracteres que es obligatorio. El canal tiene que ser de texto, del
  servidor y visible para quien reporta (si no, 404, igual que el resto de los
  endpoints de canales); el usuario reportado tiene que ser miembro y no uno
  mismo. `servers` no puede comprobar que el mensaje exista, porque el
  contenido es de `messaging`: guarda el canal y el id que mandó el cliente.
  Un reporte nace `pending` y se cierra una sola vez como `resolved` (el
  moderador actuó) o `dismissed` (no había nada que hacer) con
  `PATCH /v1/servers/{serverId}/reports/{reportId}`; queda guardado quién y
  cuándo. No dispara ninguna acción por sí solo: para borrar el mensaje,
  expulsar o banear se usan los endpoints que ya existen.
  - **Quién los ve (CA 1)**: los **moderadores**, que son quienes tienen
    `MANAGE_MESSAGES`, `KICK_MEMBERS` y `BAN_MEMBERS` **juntos** (los
    administradores y el dueño los tienen implícitos). Tener solo uno o dos
    no alcanza (403 `missing_permission`). `GET /v1/servers/{serverId}/reports`
    lista de más nuevo a más viejo; `status` filtra por `pending` (el valor por
    defecto), `resolved`, `dismissed` o `all`, y `total` es la cantidad con ese
    estado, así que `?status=pending&limit=1` alcanza para saber cuántos hay
    por revisar sin bajar la lista. Cada reporte trae el perfil replicado de
    quien reportó y del reportado (`null` hasta que llega desde identify). La
    regla vive en un solo lugar, `service.RequireModerator`.
  - **Duplicados (CA 2)**: un miembro no puede tener dos reportes `pending`
    sobre el mismo mensaje o el mismo usuario (409 `report_already_pending`).
    Lo decide la base con un índice único parcial por tipo, así que dos
    pedidos simultáneos no se cuelan. Cuando el reporte anterior se cierra se
    puede volver a reportar. Otro miembro sí puede reportar lo mismo.
  - Los reportes se borran con el servidor y, los de mensajes, con su canal.
    Si el reportado se va o lo echan, el reporte queda. Borrarlos no publica
    nada: `admin` purga los suyos al recibir `servers.server_deleted`. El aviso
    a los moderadores lo resuelve el cliente consultando el `total` pendiente.
  - Crear y cerrar un reporte publica `servers.report_updated` (ver "Expediente
    de investigación de admin").

Eventos que publica el servicio (exchange `discordia.events`, contratos en
`github.com/discordia-grupo01/events-contract` y AsyncAPI en `discordia-docs`):
`servers.member_joined`, `servers.member_left`, `servers.member_banned`,
`servers.member_unbanned`, `servers.member_kicked`, `servers.channel_created`,
`servers.channel_deleted`, `servers.channel_access_revoked`,
`servers.server_deleted`, `servers.member_muted`, `servers.member_unmuted` y
`servers.word_filter_updated`,
`servers.server_updated`,
más cinco **fotos completas** con las que `messaging` arma su réplica de
permisos y decide sin llamar acá (ADR 0003):

| Evento | Se publica cuando |
|---|---|
| `servers.owner_updated` | se crea el servidor y se acepta una transferencia de propiedad |
| `servers.role_updated` | se crea un rol o cambian sus permisos, nombre o color (viajan los permisos por nombre y `is_everyone`) |
| `servers.role_deleted` | se borra un rol (el consumidor lo saca de miembros y overrides) |
| `servers.member_roles_updated` | alguien se une (creando el servidor o por invitación) o se le asigna o quita un rol: todos sus roles, `@everyone` incluido |
| `servers.channel_overrides_updated` | se crea, edita o borra un override: todos los overrides del canal (lista vacía si no queda ninguno) |

Además, `servers.server_updated` (`server_id`, `name`, `owner_id`, `created_at`)
es la foto del servidor que `admin` usa para su listado de servidores: se
publica al crear el servidor, al cambiarle el nombre y al aceptar una
transferencia de propiedad (cambiar solo el ícono o el banner no la publica).
No lleva `version`: `admin` se queda con la de `occurred_at` más nuevo. La
migración `0026_backfill_server_updated` la publica para todos los servidores
ya existentes y se puede repetir sin riesgo.

Cada foto se escribe en el outbox en la **misma transacción** que el cambio
(`repository/access_events.go`) y lleva el estado ya escrito, así que un
consumidor puede descartar la que llegue más vieja que la que tiene. La
migración `0022_backfill_access_events` re-publica todo lo existente: hay que
correrla con la cola del consumidor ya creada, o repetirla después.

### Expediente de investigación de admin (HU #81)

Para que el staff vea reportes y moderación de un usuario o un servidor sin
que `admin` nos llame (ADR-024), el servicio publica dos eventos más, siempre en
la **misma transacción** que la escritura:

| Evento | Se publica cuando | Lleva |
|---|---|---|
| `servers.report_updated` | se crea un reporte y se lo cierra | foto completa: `report_id`, `server_id`, `kind`, `reported_user_id`, `channel_id`, `message_id`, `reason`, `status`, `created_at`, `resolved_at` (los que no aplican, `null`) |
| `servers.moderation_action_recorded` | se escribe una fila de `moderation_audit_entries` (ban, unban, kick, mute, unmute, **incluido el fin automático de un silencio**) | `action_id` (el id de la fila, clave de idempotencia), `server_id`, `action`, `target_user_id` |

- Ninguno lleva a quien reportó, a quien resolvió ni al moderador que actuó
  (minimización de datos): `admin` no los necesita.
- `report_updated` es una foto: `admin` se queda con la de `occurred_at` más
  nuevo (la de creación, o la de cierre cuando está cerrado).
  `moderation_action_recorded` es inmutable.
- `insertAudit` (`moderation_repo`) es el único lugar donde se escribe la
  auditoría, y ahí se publica: una acción nueva no puede quedar sin evento. Es un
  evento aparte de `member_banned`, `member_muted`, etc. a propósito: hacer el
  backfill de esos re-ejecutaría efectos en sus consumidores (por ejemplo,
  volver a silenciar a alguien).
- **Backfill**: `0034_backfill_report_events` publica la foto actual de cada
  reporte y `0035_backfill_moderation_actions` una acción por cada fila de
  auditoría (con su fecha original). Se pueden repetir sin riesgo.
- **Orden de despliegue**: `admin` tiene que estar desplegado **antes**. Si la
  cola de `admin` todavía no existe cuando corren `0034` y `0035`, RabbitMQ
  descarta esos eventos; en ese caso se vuelve a ejecutar el `INSERT` de la
  migración a mano.
- Un reporte de mensaje cuyo **canal** se borra desaparece de acá (CASCADE) sin
  evento; `admin` lo conserva hasta que se borre el servidor.

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

## Usuarios suspendidos

`discordia-admin` suspende y reactiva cuentas con `admin.user_suspended` y
`admin.user_reactivated` (firmados con `ADMIN_EVENTS_HMAC_SECRET`). El consumidor
(`internal/events/admin_consumer`, cola `servers.admin_events`) los guarda en
`suspended_users` solo si traen una `version` mayor a la guardada. El middleware
`RejectSuspended`, que corre después de `Auth` en todo `/v1` autenticado, responde
`403` a cualquier request de un suspendido, con el formato de error habitual y el
motivo en `details.error_code`:

```json
{ "error": { "code": 403, "message": "account suspended", "details": { "error_code": "ACCOUNT_SUSPENDED" } } }
```

- Incluye `GET /v1/channels/{id}/voice-access`, que el servicio de voz usa para
  autorizar el token: así la entrada a voz queda bloqueada sin tocar ese servicio.
- El estado se cachea en memoria 5 segundos por usuario; la instancia que aplica
  el evento invalida la entrada al instante, las demás se enteran dentro de esa
  ventana. Si la base no responde, el request se rechaza con `503` (no se deja
  pasar a alguien que podría estar suspendido).
- El suspendido sigue siendo miembro de sus servidores y no se le cambia el rol.
  En `GET /v1/servers/{id}/members` (y en el listado de miembros de un rol) sigue
  figurando, pero con `profile: null`: no se devuelven su nombre ni su avatar
  mientras dure la suspensión. Reactivar levanta todo con el siguiente evento.
- Rutas públicas (`/livez`, `/readyz`, íconos y banners) no se ven afectadas.

## Servidores suspendidos o eliminados por la plataforma

El staff modera servidores desde el backoffice (HU #68 y #69). `discordia-admin`
registra la decisión y la publica firmada (`ADMIN_EVENTS_HMAC_SECRET`); este
servicio la ejecuta. El consumidor es el mismo de arriba (cola
`servers.admin_events`, a la que se suman tres bindings) y descarta, sin cambiar
nada, todo evento con firma ausente o inválida, `server_id` que no sea un UUID o
`version < 1`.

| Evento | Qué hace este servicio |
|---|---|
| `admin.server_suspended` | guarda `servers.suspended_at` (el `occurred_at` del evento) si su `version` es mayor a `moderation_version` |
| `admin.server_reactivated` | pone `suspended_at` en `NULL` con la misma regla de versión |
| `admin.server_deletion_requested` | borra el servidor (cascada de la HU #40) y publica `servers.server_deleted`. No compara versión: es irreversible y es idempotente, el segundo pedido ya no encuentra el servidor |

**Servidor suspendido.** El middleware `RejectSuspendedServer` corre después de
`Auth` y de `RejectSuspended` y toma el servidor del primer parámetro de la ruta
entre `serverId`, `channelId`, `categoryId`, `roleId` y `code` (invitación). Si está
suspendido responde, **también al owner**:

```json
{ "error": { "code": 403, "message": "server suspended", "details": { "error_code": "SERVER_SUSPENDED" } } }
```

- Cubre todo: ver el servidor, membresía, miembros, invitar y unirse (la invitación
  no se consume), editar, transferir, eliminar, roles, canales, categorías, permisos,
  moderación y `GET /v1/channels/{id}/voice-access`, así que nadie obtiene un token
  de voz de un servidor suspendido. Un test recorre **todas** las rutas del router y
  falla si alguna nueva queda sin protección.
- **Excepciones:** `POST` y `GET /v1/servers` (no nombran ningún servidor) y que un
  miembro se **abandone** a sí mismo (`DELETE /v1/servers/{id}/members/{su id}`).
  Sacar a otro sigue bloqueado. Los íconos y banners públicos no llevan JWT.
- **El owner no puede irse:** abandonar un servidor siendo owner ya daba
  `409 owner_must_transfer_or_delete`, y transferir y eliminar están bloqueados,
  así que no tiene salida hasta que el staff reactive o elimine. Es a propósito:
  durante la investigación nadie puede borrar evidencia.
- `GET /v1/servers` sigue listándolo, con `suspended: true` y **sin canales ni
  categorías**. En el resto de las respuestas `suspended` es siempre `false`.
- La caché es por recurso (5 s) y se vacía entera cuando esta instancia aplica un
  evento; las demás instancias se enteran dentro de esos 5 s. Si la base no responde,
  `503`: nunca deja pasar. Un id mal formado o un recurso que no existe pasa al
  handler, que contesta `400` o `404`.
- Los trabajos en segundo plano (vencimiento de transferencias y de silencios) no
  distinguen servidores suspendidos.

**Despliegue.** Todas las instancias comparten la cola, así que una instancia
anterior a este cambio descartaría `admin.server_*` ("routing key desconocida").
Desplegar `servers` completo **antes** que `admin`. Una suspensión perdida se
recupera con `make republish-suspensions` en admin; una eliminación perdida se
repite desde el backoffice (el servidor sigue visible).

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
| GET | `/v1/servers/{serverId}/membership` | si | Si el caller es dueño o miembro del servidor, en una sola consulta y sin cargar el servidor. `200 {"server_id", "member": true}`; `403` si no es miembro (a diferencia de `GET /v1/servers/{serverId}`, que a un no miembro le devuelve el servidor con `channels: []`); `404` si no existe. Lo usa el servicio de voz para autorizar la consulta de participantes. |
| PATCH | `/v1/servers/{serverId}` | si (admin) | Editar nombre, ícono y/o banner (multipart: `name?`, `icon?`, `banner?`, `remove_banner?`). Todos los campos son opcionales; sin permisos da 403, datos invalidos dan 400 sin persistir nada. |
| DELETE | `/v1/servers/{serverId}` | si (owner) | Eliminar el servidor y todo lo suyo (cuerpo JSON `confirm_name` con el nombre exacto). 204; 400 si falta o no coincide la confirmación; 403 si no es el owner; 404 si no existe. Irreversible. |
| GET | `/v1/servers/{serverId}/icon` | no (público) | Sirve el archivo del ícono subido. Si el servidor no tiene ícono, `icon_url` es `null` y los clientes muestran el fallback visual con la inicial. En producción (Supabase) `icon_url` apunta directo al bucket público; esta ruta queda como fallback para setups sin Supabase. |
| GET | `/v1/servers/{serverId}/banner` | no (público) | Sirve el archivo del banner. 404 si el server no tiene uno. Mismo esquema de URL que el icono. |
| GET | `/v1/servers/{serverId}/members` | si (miembro) | Listar los miembros del server (paginado), cada uno con su `profile` (`null` si todavía no se replicó) y `muted_until` (hasta cuándo está silenciado, `null` si no lo está) y `role_ids` (ids de sus roles, @everyone incluido; se cruzan con `GET /roles` en el cliente). |
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
| GET | `/v1/roles/{roleId}/members` | si (`MANAGE_ROLES`) | Miembros que tienen ese rol (paginado, misma forma que `GET /v1/servers/{serverId}/members`). Para `@everyone`, todos los miembros. 404 si el rol no existe. |
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
| POST | `/v1/servers/{serverId}/reports` | si (miembro) | Reportar un mensaje (`kind: "message"`, `channel_id`, `message_id`) o un usuario (`kind: "user"`, `user_id`) con un `reason` obligatorio de hasta 512 caracteres. 201 con el reporte (`pending`); 400 si falta algo, el canal no es de texto o es uno mismo; 403 si no es miembro; 404 si el canal no existe o no lo ve, o el usuario no es miembro; 409 `report_already_pending` si ya tiene uno pendiente sobre eso. Ver "Moderación". |
| GET | `/v1/servers/{serverId}/reports` | si (`MANAGE_MESSAGES` + `KICK_MEMBERS` + `BAN_MEMBERS`) | Listar los reportes (paginado: `limit`, `offset`; más nuevo primero). `status`: `pending` (por defecto), `resolved`, `dismissed` o `all`. `total` es la cantidad con ese estado. |
| PATCH | `/v1/servers/{serverId}/reports/{reportId}` | si (`MANAGE_MESSAGES` + `KICK_MEMBERS` + `BAN_MEMBERS`) | Cerrar un reporte pendiente (`status`: `resolved` o `dismissed`). 200 con el reporte; 404 si no existe; 409 `report_not_pending` si ya estaba cerrado. |
| PUT | `/v1/channels/{channelId}/permissions/roles/{roleId}` | si (`MANAGE_ROLES`) | Crear o reemplazar el override de un rol en un canal (`allow?`, `deny?`: `VIEW_CHANNELS`, `SEND_MESSAGES`, `MANAGE_MESSAGES`, `ADD_REACTIONS`, `CONNECT`). 200 con el override; 204 si las dos listas vienen vacías (se borra). 400 si el permiso no se puede sobrescribir o está en las dos listas; 403 si el rol no está por debajo del propio (salvo `@everyone`) o se permite algo que el caller no tiene. |
| DELETE | `/v1/channels/{channelId}/permissions/roles/{roleId}` | si (`MANAGE_ROLES`) | Quitar el override de un rol en un canal. 204; 404 `channel_override_not_found` si no había. |
| GET | `/v1/channels/{channelId}/permissions` | si (`MANAGE_ROLES`) | Listar los overrides del canal (`@everyone` primero, después por jerarquía). |
| GET | `/v1/channels/{channelId}/members/{userId}/permissions` | si (miembro) | Permisos efectivos del miembro en ese canal (con los overrides aplicados). Misma forma y códigos que el endpoint por servidor. |
| POST | `/v1/servers/{serverId}/mentions/resolve` | si (miembro) | Para `messaging`: valida las menciones de un mensaje (`channel_id`, `user_ids`, `role_ids`, `everyone`) y devuelve cuáles valen y a quién notificar (`recipient_ids`). Roles y `@everyone` solo cuentan con `MENTION_EVERYONE`. Ver "Menciones". |
| GET | `/livez` `/readyz` | no | Health checks. |

## Health checks
- `GET /livez` - el proceso esta vivo.
- `GET /readyz` - el servicio puede operar (pinguea la base; 503 si no responde).

## Logs

Cada línea que escribe el servicio es un objeto JSON por la salida estándar, con `time`, `level` (`debug`, `info`, `warning` o `error`), `service` (`servers`) y `message`. Es el mismo formato que usan `messaging`, `notifications` y `voice-sfu`.

- **Nivel configurable** con `LOG_LEVEL` (`debug`, `info`, `warning` o `error`; por defecto `info`). Un valor inválido hace fallar el arranque con un mensaje claro. Al arrancar escribe `service_started` con el nivel elegido.
- **Qué significa cada nivel:**
  - `error`: falla que impide ejecutar la funcionalidad (no se pudo guardar, la base no responde, un evento agotó los reintentos).
  - `warning`: condición anómala que no impide la funcionalidad básica (un evento descartado por inválido o por firma mala, una conexión a RabbitMQ que se reintenta, una configuración opcional que falta).
  - `info`: acción iniciada por una persona o por el sistema y trazable (evento procesado, barrido de vencimientos, pedido HTTP).
  - `debug`: contexto técnico para diagnosticar (eventos ignorados por viejos o repetidos, `/livez` y `/readyz` cuando responden bien).
- **Pedidos HTTP:** se registran como `http_request` con `method`, `route`, `status` y `duration_ms`. `route` es el **patrón** (`/v1/servers/:serverId`), nunca la ruta real ni la query string, porque ahí viajan ids, invitaciones y tokens. Un pedido sin ruta sale como `unmatched`. Un 5xx se registra como `error`.
- **Qué no se escribe:** el valor de campos llamados `password`, `token`, `ticket`, `jwt`, `authorization`, `cookie`, `secret`, `api_key`, `email` o `content` sale como `[FILTERED]`. Como red de seguridad, cualquier email o JWT que aparezca dentro de un texto (por ejemplo, en el mensaje de un error de la base) se reemplaza por `[EMAIL]` o `[JWT]`.
- **Pánicos:** se registran como `panic_recovered` con la ruta, el error y la traza, sin volcar los headers del pedido.
- El código vive en `internal/logging`. Las llamadas viejas al paquete `log` de Go también salen como JSON, porque `Setup` redirige el logger estándar.

## Trazas y métricas (OpenTelemetry)

El servicio manda trazas y métricas a cualquier backend que hable OTLP por HTTP (New Relic, Grafana Cloud, un colector propio). Está **apagado por defecto**: sin `OTEL_EXPORTER_OTLP_ENDPOINT` no se crea ningún exportador y el servicio funciona igual.

| Variable | Para qué |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | URL base del backend. En New Relic (cuenta de EE. UU.): `https://otlp.nr-data.net` |
| `OTEL_EXPORTER_OTLP_HEADERS` | Credenciales, por ejemplo `api-key=<clave de licencia de ingesta>`. Es un secreto: va solo en las variables de entorno del despliegue, nunca en el repositorio |
| `OTEL_SERVICE_NAME` | Nombre con el que aparece el servicio (por defecto `servers`) |
| `OTEL_TRACES_SAMPLER` | Muestreo estándar de OpenTelemetry (por defecto, todas las trazas) |

- **Pedidos HTTP:** un span por pedido, llamado como el patrón de la ruta (`GET /v1/servers/:serverId`). Solo guarda `http.request.method`, `http.route` y `http.response.status_code`: nunca el path real, la query string ni los headers. Si el pedido trae un `traceparent` (Kong o un cliente), la traza continúa desde ahí.
- **Base de datos:** un span por consulta de `pgx`, con la operación y el SQL con sus `$1`, `$2`, **sin los valores**. Las consultas que no pertenecen a un pedido (como el barrido del outbox cada 2 s) no generan spans.
- **RabbitMQ:** `outbox_events.traceparent` (migración `0033`) guarda la traza del pedido que originó el evento. Cuando el *poller* lo publica crea un span `outbox publish <routing key>` dentro de esa traza y el mensaje sale con la cabecera `traceparent`, así el consumidor puede seguirla. Los eventos viejos y los que no vienen de un pedido tienen `traceparent` nulo y se publican igual.
- **Consumidores:** `admin_consumer`, `billing_consumer` y `profile_consumer` abren un span `<routing key> process` dentro de la traza que llega en la cabecera `traceparent` del mensaje; si falla y se reintenta, el span queda marcado como error. Si el mensaje no la trae, empieza una traza nueva.
- **Qué nunca se exporta:** el valor de los parámetros SQL, el texto de un error (un filtro del exportador le borra `exception.message` y `exception.stacktrace` a todo span) ni datos del usuario.
- **Métricas HTTP:** el histograma `http.server.request.duration` (en segundos, temporalidad delta, exportado cada 60 s) se registra en el mismo middleware que arma el span, con `http.request.method`, `http.route` y `http.response.status_code`; si el estado es 5xx suma `error.type`. De ahí salen el throughput, la latencia y la tasa de errores del resumen de APM en New Relic. Los pedidos sin ruta se agrupan en `unmatched`. Nunca guarda la ruta real ni la query string.
- **Logs:** cada línea escrita dentro de un pedido lleva `trace_id` y `span_id`.

## Envío de logs (New Relic)

Además de la salida estándar, el servicio puede enviar sus logs al Log API de New Relic. Está **apagado por defecto**: solo se activa si hay `LOG_SHIP_URL` y una clave (`LOG_SHIP_API_KEY` o la `api-key` de `OTEL_EXPORTER_OTLP_HEADERS`).

| Variable | Para qué |
|---|---|
| `LOG_SHIP_URL` | URL del Log API. En New Relic (cuenta de EE. UU.): `https://log-api.newrelic.com/log/v1` |
| `LOG_SHIP_API_KEY` | Opcional: clave de ingesta para `LOG_SHIP_URL`. Si no está, se toma la `api-key` de `OTEL_EXPORTER_OTLP_HEADERS`. Es un secreto: va solo en las variables de entorno del despliegue |

- **Qué se envía:** las mismas líneas que salen por la salida estándar, con el mismo filtrado de datos sensibles (campos `password`, `token`, `ticket`, `email`, `content`, etc. y emails o JWT dentro de textos). `trace_id` y `span_id` viajan como `trace.id` y `span.id`, que es como New Relic enlaza cada log con su traza. El servicio se identifica con `service.name` (el valor de `OTEL_SERVICE_NAME`, por defecto `servers`).
- **Cómo se envía:** `internal/logging` junta las líneas en lotes de hasta 200 o cada 5 segundos, las comprime en gzip y las manda por HTTPS. Los pedidos no esperan al envío.
- **Si el backend falla:** reintenta con espera creciente (de 1 s hasta 60 s) ante errores de red, 408, 429 y 5xx; un lote rechazado de forma definitiva (por ejemplo 400 o 403) se descarta. La cola está acotada a 5.000 entradas: si se llena descarta las más viejas, para no crecer sin límite en memoria.
- **Sin bucles:** los avisos del propio envío (`log_shipping_failed`, `log_shipping_recovered`, `log_batch_discarded`, `log_entries_dropped`) salen solo por la salida estándar y nunca se reenvían.
- **Al apagar:** `logging.Flush()` intenta enviar lo que quedó pendiente durante unos 3 segundos.

## Formateo automatico
Este repo se autoformatea con gofmt en cada commit. Activalo una vez por clone:
```bash
git config core.hooksPath .githooks
```
Si usan VS Code con la extension oficial de Go, tambien formatea al guardar (ya configurado en .vscode/settings.json).

El CI corre `gofmt -l` como red de seguridad (falla si encuentra archivos sin formatear), por si alguien clono sin activar el hook.
