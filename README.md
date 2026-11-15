# discordia-servers

Servicio de servidores de Discordia.

## Tabla de contenidos

- [Introducción](#introducción)
- [Pre-requisitos](#pre-requisitos)
- [Cómo correr](#cómo-correr)
- [Variables de entorno](#variables-de-entorno)
- [Comandos Docker](#comandos-docker)
- [Tests](#tests)
- [Más documentación](#más-documentación)

## Introducción

`servers` es el servicio que maneja todo lo que pasa dentro de un servidor de Discordia. Se divide en cuatro partes:

- **Servidores:** crear un servidor, invitar gente con un link, unirse, abandonar y transferir la propiedad.
- **Canales:** canales de texto y de voz, organizados en categorías, con permisos propios por canal.
- **Roles y permisos:** crear roles, darles permisos, ordenarlos por jerarquía y asignarlos a los miembros.
- **Moderación:** banear, expulsar y silenciar miembros, denunciar mensajes y usuarios, filtrar palabras y llevar un registro de lo que hizo cada moderador.

Está hecho en Go con Gin y guarda todo en su propia base PostgreSQL, que ningún otro servicio lee. Cuando algo cambia, avisa por RabbitMQ para que los demás servicios se enteren (por ejemplo, `messaging` necesita saber qué rol tiene cada miembro para decidir si puede escribir en un canal).

El JWT lo valida el gateway (Kong) y este servicio solo toma el id del usuario. La API REST está descripta en [`openapi.yaml`](openapi.yaml).

## Pre-requisitos

- **Docker** y **Docker Compose** v2.24 o más nuevo.
- **Go 1.22**, solo si querés correr el servicio con Go en tu máquina.
- **RabbitMQ**, solo si querés probar los eventos. Se levanta con el repositorio [`rabbit-mq`](https://github.com/discordia-grupo01/rabbit-mq).
- **make**.
- **Node.js**, solo para regenerar el `openapi.yaml` con `make openapi`.

## Cómo correr

### Con Docker Compose

Levanta el servicio y su base PostgreSQL, sin configurar nada:

```bash
docker compose up --build
```

Antes de arrancar el servicio, un contenedor `migrate` aplica las migraciones pendientes de la carpeta `migrations/` y termina. Cuando todo esté arriba, comprobá que el servicio esté vivo:

```bash
curl http://localhost:8080/readyz
```

Para probarlo sin JWT, mandá el header `X-User-Id` (en el compose ya está activado). Por ejemplo, para crear un servidor:

```bash
curl -X POST http://localhost:8080/v1/servers \
  -H "X-User-Id: 11111111-1111-4111-8111-111111111111" \
  -F "name=Mi servidor"
```

Si querés cambiar puertos, usuario de la base o los secretos de prueba, copiá el archivo de ejemplo y editalo antes de levantar:

```bash
cp .env.example .env
```

Este compose levanta solo lo que es de este servicio: la aplicación y su base. RabbitMQ no está incluido porque es de otro repositorio. Para el sistema completo (con el gateway y los demás servicios) usá el compose del repositorio `infrastructure`.

### Con eventos (RabbitMQ)

Sin RabbitMQ el servicio funciona igual, pero no publica ni consume eventos. Para probarlos, levantá primero RabbitMQ desde el repositorio `rabbit-mq`:

```bash
cd ../rabbit-mq
docker compose up -d
```

Después, en el `.env` de este repositorio, completá la variable según cómo corras el servicio:

| Cómo lo corrés | Qué completar en `.env` |
|---|---|
| Con `docker compose up` | `RABBITMQ_URL_DOCKER=amqp://guest:guest@host.docker.internal:5672/` |
| Con `make run` | `RABBITMQ_URL=amqp://guest:guest@localhost:5672/` |

Desde un contenedor, `localhost` es el propio contenedor, por eso hace falta `host.docker.internal`.

### Para desarrollar

Si vas a tocar el código, te conviene correr el servicio con Go en tu máquina (así recompilás rápido y podés usar el debugger) y dejar en un contenedor solo la base:

```bash
cp .env.example .env
docker compose up -d db
make run
```

Con el `.env` de ejemplo, el servicio se conecta a la base que acabás de levantar. Si usás otra base, aplicá las migraciones a mano con `make migrate`.

## Variables de entorno

Se leen del entorno o de un archivo `.env`. El archivo [`.env.example`](.env.example) trae valores listos para correr en local.

| Variable | Default | Descripción |
|---|---|---|
| `ENVIRONMENT` | `development` | `development` o `production`. |
| `HOST` | `0.0.0.0` | IP donde escucha el servicio. |
| `PORT` | `8080` | Puerto donde escucha el servicio. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warning` o `error`. |
| `DATABASE_HOST` | obligatoria | Host de PostgreSQL. |
| `DATABASE_PORT` | obligatoria | Puerto de PostgreSQL. |
| `DATABASE_NAME` | obligatoria | Nombre de la base. |
| `DATABASE_USER` | obligatoria | Usuario de la base. |
| `DATABASE_PASSWORD` | obligatoria | Contraseña de la base. |
| `DATABASE_SSLMODE` | `disable` | `disable` en local, `require` con Supabase. |
| `DATABASE_SCHEMA` | vacío | Schema del servicio dentro de la base. Vacío en local; en Supabase es `servers`. |
| `DATABASE_MAX_CONNS` | `0` | Máximo de conexiones del pool. Con `0` se usa el default de pgx. |
| `USER_ID_HEADER` | vacío | Con `X-User-Id`, el servicio toma el usuario de ese header en vez del JWT. Solo para desarrollo: nunca en un entorno desplegado, porque Kong no filtra ese header y cualquiera podría hacerse pasar por otro usuario. |
| `ICON_DIR` | `./data/icons` | Carpeta donde se guardan los íconos y banners, si no se usa Supabase Storage. |
| `ICON_MAX_BYTES` | `5242880` | Tamaño máximo de un ícono o banner, en bytes. |
| `SUPABASE_STORAGE_URL` | vacío | URL del proyecto de Supabase. Junto con las dos siguientes, manda los íconos a un bucket en vez de a `ICON_DIR`. |
| `SUPABASE_STORAGE_BUCKET` | vacío | Nombre del bucket. |
| `SUPABASE_SERVICE_KEY` | vacío | Clave `service_role` del proyecto. Es un secreto. |
| `RABBITMQ_URL` | vacío | Conexión a RabbitMQ. Si está vacía el servicio funciona igual, pero no publica ni consume eventos. |
| `ADMIN_EVENTS_HMAC_SECRET` | vacío | Secreto compartido con `admin` para verificar las suspensiones. Obligatorio si hay `RABBITMQ_URL`. |
| `BILLING_EVENTS_HMAC_SECRET` | vacío | Secreto compartido con `billing` para la saga de boosts. Obligatorio si hay `RABBITMQ_URL`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | vacío | Backend OpenTelemetry (por ejemplo New Relic). Vacío = sin trazas ni métricas. |
| `OTEL_EXPORTER_OTLP_HEADERS` | vacío | Credenciales del backend. Es un secreto. |
| `LOG_SHIP_URL` | vacío | Si se completa, también manda los logs a New Relic. |

Variables que usa solo `docker-compose.yml`:

| Variable | Default | Descripción |
|---|---|---|
| `API_PORT` | `8080` | Puerto del servicio en tu máquina. |
| `DATABASE_PORT` | `5432` | Puerto de PostgreSQL en tu máquina. |
| `RABBITMQ_URL_DOCKER` | vacío | Conexión a RabbitMQ para el servicio que corre dentro del compose (ver [Con eventos](#con-eventos-rabbitmq)). |

## Comandos Docker

Todos desde la raíz del repositorio.

| Qué querés hacer | Comando |
|---|---|
| Levantar todo | `docker compose up --build` |
| Levantar todo en segundo plano | `docker compose up -d --build` |
| Levantar solo la base | `docker compose up -d db` |
| Ver los logs del servicio | `docker compose logs -f api` |
| Ver qué está corriendo | `docker compose ps` |
| Reconstruir solo el servicio | `docker compose up -d --build api` |
| Abrir una consola de la base | `docker compose exec db psql -U discordia -d discordia_servicio` |
| Bajar todo, conservando los datos | `docker compose down` |
| Bajar todo y borrar los datos | `docker compose down -v` |

Las migraciones nuevas se aplican solas cada vez que levantás el compose. `make db-reset` borra los datos y deja solo la base corriendo; después, `make migrate` aplica las migraciones.

Para construir y correr la imagen a mano, contra una base que ya tengas:

```bash
docker build -t discordia-servers .
docker run --rm -p 8080:8080 --env-file .env discordia-servers
```

Dentro de un contenedor, `DATABASE_HOST` y `RABBITMQ_URL` no pueden apuntar a `localhost`: tienen que usar el nombre del contenedor o `host.docker.internal`.

## Tests

Los tests unitarios no necesitan nada más:

```bash
make test
```

Los de integración (`test/repository`, `test/e2e` y `test/events`) usan Postgres y RabbitMQ reales y se saltean solos si no encuentran las variables de conexión. Cuidado: borran y recrean las tablas de la base al empezar y al terminar, así que no los corras contra una base que uses para probar a mano. Creá una aparte, con RabbitMQ levantado desde el repositorio `rabbit-mq`:

```bash
docker compose up -d db
docker compose exec db psql -U discordia -c "CREATE DATABASE discordia_test"

DATABASE_HOST=localhost DATABASE_PORT=5432 DATABASE_NAME=discordia_test \
DATABASE_USER=discordia DATABASE_PASSWORD=discordia \
RABBITMQ_URL=amqp://guest:guest@localhost:5672/ \
go test ./... -p 1
```

El `-p 1` es necesario: varios paquetes de integración usan la misma base y se pisarían entre sí si corrieran en paralelo.

El CI exige una cobertura mínima del 70 % y corre toda la suite, integración incluida, en cada pull request. También hay mutation testing de la capa de servicios (`make mutation`, tarda unos 45 minutos y no corre en el CI).

El repositorio se formatea con gofmt. Activá el hook una vez por clon, y el CI falla si hay archivos sin formatear:

```bash
git config core.hooksPath .githooks
```

## Más documentación

En este repositorio:

- [`openapi.yaml`](openapi.yaml): contrato de la API REST. Se genera con `make openapi`, no se edita a mano.
- [`docs/historias-de-usuario.md`](docs/historias-de-usuario.md): las historias del catálogo que resuelve este servicio, con sus rutas.
- [`docs/mutation-testing`](docs/mutation-testing/README.md): resultados del mutation testing.

En el repositorio [`docs`](https://github.com/discordia-grupo01/docs):

- [`asyncapi`](https://github.com/discordia-grupo01/docs/tree/main/asyncapi): los eventos que este servicio publica y consume.
