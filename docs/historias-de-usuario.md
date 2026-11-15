# Historias de usuario

Las historias del catálogo de la cátedra que resuelve este servicio, con su número, y las que resuelve junto con otros servicios. Las rutas completas con sus parámetros y respuestas están en [`openapi.yaml`](../openapi.yaml).

## Obligatorias

| # | Historia | Puntos | Rutas |
|---|---|---|---|
| 6 | Crear servidor | 3 | `POST /v1/servers` |
| 7 | Generar invitación | 5 | `POST /v1/servers/{serverId}/invites`, `GET /v1/servers/{serverId}/invites`, `DELETE /v1/invites/{code}` |
| 8 | Unirse a servidor vía invitación | 3 | `POST /v1/invites/{code}/join` |
| 9 | Abandonar servidor | 2 | `DELETE /v1/servers/{serverId}/members/{userId}` |
| 10 | Transferir propiedad del servidor | 3 | `POST /v1/servers/{serverId}/ownership-transfers` y sus rutas `accept`, `reject` y `cancel` |
| 11 | Crear canal | 3 | `POST /v1/servers/{serverId}/channels` |
| 12 | Editar canal | 2 | `PATCH /v1/channels/{channelId}` |
| 13 | Eliminar canal | 2 | `DELETE /v1/channels/{channelId}` |
| 14 | Organizar canales en categorías | 3 | `POST /v1/servers/{serverId}/categories`, `PATCH /v1/channels/{channelId}/category` |
| 21 | Crear rol | 3 | `POST /v1/servers/{serverId}/roles` |
| 22 | Editar permisos de un rol | 5 | `PATCH /v1/roles/{roleId}` |
| 23 | Asignar rol a un miembro | 3 | `POST /v1/servers/{serverId}/members/{userId}/roles` |
| 24 | Definir rol por defecto para nuevos miembros | 2 | `PUT /v1/servers/{serverId}/default-role` |
| 25 | Banear miembro del servidor | 3 | `POST /v1/servers/{serverId}/bans` |
| 26 | Revocar baneo | 2 | `DELETE /v1/servers/{serverId}/bans/{userId}` |

## Optativas

| # | Historia | Puntos | Rutas |
|---|---|---|---|
| 39 | Administrar configuración del servidor | 3 | `PATCH /v1/servers/{serverId}`, `PATCH /v1/servers/{serverId}/vanity` |
| 40 | Eliminar servidor | 5 | `DELETE /v1/servers/{serverId}` |
| 41 | Reordenar canales | 2 | `PATCH /v1/servers/{serverId}/channels/reorder`, `PATCH /v1/servers/{serverId}/categories/reorder` |
| 55 | Eliminar rol | 3 | `DELETE /v1/roles/{roleId}` |
| 56 | Reordenar jerarquía de roles | 3 | `PATCH /v1/servers/{serverId}/roles/reorder` |
| 57 | Sobrescribir permisos de un rol en un canal específico | 5 | `PUT` y `DELETE /v1/channels/{channelId}/permissions/roles/{roleId}` |
| 58 | Ver miembros por rol | 2 | `GET /v1/roles/{roleId}/members` |
| 59 | Expulsar miembro del servidor | 3 | `POST /v1/servers/{serverId}/kicks` |
| 60 | Silenciar temporalmente a un miembro | 5 | `POST /v1/servers/{serverId}/mutes`, `DELETE /v1/servers/{serverId}/mutes/{userId}` |
| 61 | Ver registro de auditoría de moderación | 3 | `GET /v1/servers/{serverId}/moderation-audit` |
| 75 | Gestionar emojis personalizados del servidor | 5 | `POST`, `GET` y `DELETE /v1/servers/{serverId}/emojis` |
| 80 | Ver beneficios de servidor boosteado | 3 | `GET /v1/servers/{serverId}/boosts` |

## Historias en las que participa

Estas historias las completan otros servicios. Acá está la parte que le toca a `servers`:

| # | Historia | Qué hace este servicio |
|---|---|---|
| 17 | Unirse a canal de voz de servidor | Dice si el usuario puede entrar al canal: `GET /v1/channels/{channelId}/voice-access`. |
| 46 | Menciones | Resuelve a quién menciona un mensaje: `POST /v1/servers/{serverId}/mentions/resolve`. |
| 62 | Reportar mensaje o usuario | Guarda los reportes y deja a los moderadores revisarlos: `POST`, `GET` y `PATCH /v1/servers/{serverId}/reports`. |
| 63 | Filtro automático de palabras prohibidas | Guarda la lista de palabras de cada servidor (`GET` y `PUT /v1/servers/{serverId}/banned-words`). `messaging` es quien la aplica. |
| 68 | Eliminar servidor desde el backoffice | Recibe el evento firmado de `admin` y borra el servidor. |
| 69 | Suspender servidor completo | Recibe el evento firmado de `admin` y marca el servidor como suspendido. |
| 79 | Usar puntos para boostear un servidor | Aplica o rechaza el boost que pide `billing`, por eventos. |
| 81 | Ver expediente de investigación de usuario o servidor | Publica los reportes y las acciones de moderación para que `admin` arme el expediente. |

## Decisiones técnicas

- **ADR 0009, reportes de mensajes y usuarios:** explica cómo se guardan y se revisan los reportes (historia 62).
- **ADR 0003, réplica local de permisos:** explica por qué `messaging` guarda su propia copia de los roles y permisos que publica este servicio (historias 21 a 24 y 57).
