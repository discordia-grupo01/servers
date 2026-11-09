# Mutation testing de servers

## Qué es y para qué sirve

La cobertura dice qué líneas **se ejecutan** durante los tests, pero no si los
tests **revisan** lo que pasa en ellas. El mutation testing mide eso otro: la
herramienta mete a propósito errores chicos en el código, uno por vez (cambia
`>` por `>=`, niega un `if`, cambia `||` por `&&`, un `continue` por `break`...),
y corre los tests con cada uno. Cada versión con un error se llama **mutante**:

- Si algún test falla, el mutante quedó **muerto** (`KILLED`): los tests
  detectaron el error.
- Si todos pasan, **sobrevivió** (`LIVED`): ese error podría llegar a
  producción sin que nada avise.

La **efectividad** es muertos / (muertos + sobrevivientes).

## Herramienta y alcance

Usamos [gremlins](https://github.com/go-gremlins/gremlins), una herramienta de
mutation testing para Go, sobre `internal/service`: las reglas de servidores,
roles y permisos, canales, moderación, invitaciones y menciones. Activamos
también los mutantes opcionales (operadores lógicos, asignaciones,
`continue`/`break`, operaciones de bits).

## Cómo correrlo

```bash
go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
make mutation
```

Tarda unos 45 minutos y no está en el CI. Dos cosas que aprendimos:

- **Los tests de los servicios están en `test/service/`, no al lado del
  código.** Por defecto gremlins corre solo los tests del mismo paquete que el
  código mutado, y acá no hay ninguno: todos los mutantes "sobrevivirían". Por
  eso usamos `--integration`, que corre toda la suite con cada mutante, y
  `--coverpkg` para que cuente la cobertura que generan esos tests. Es lo que
  lo hace lento.
- **Sin `DATABASE_HOST` ni `RABBITMQ_URL`.** Con `--integration` también
  corren los tests de integración, que hacen `DROP TABLE` contra la base que
  encuentran. `make mutation` saca esas variables (que llegan del `.env`) para
  que se salteen, porque si no, borrarían esa base cientos de veces.
- **Limpiar la caché de tests antes** (`go clean -testcache`, ya está en el
  target). gremlins calcula cuánto esperar a cada mutante a partir de lo que
  tardan los tests; si Go devuelve el resultado guardado en caché, mide casi
  cero y muchos mutantes terminan en `TIMED OUT` sin que les pase nada.

## Resultados

Archivos `before.*` (primera corrida) y `after.*` (después de agregar tests).

| | Antes | Después |
|---|---|---|
| Muertos | 507 | 529 |
| Sobrevivientes | 28 | 9 |
| No cubiertos | 24 | 24 |
| Timeout | 3 | 0 |
| **Efectividad** | **94,77%** | **98,33%** |

Los 9 que siguen vivos son los equivalentes de la tabla de abajo: sin
contarlos, los tests detectan **todos** los mutantes que cambian el
comportamiento. Los 3 timeouts de la primera corrida (`authz.go`) en la
segunda mueren de forma normal.

### Los 28 sobrevivientes de la primera corrida

**19 eran tests que faltaban** (ya están agregados):

| Dónde | El error que nadie detectaba | Test nuevo |
|---|---|---|
| `role_service.go:82`, `244`; `category_service.go:50`; `channel_service.go:181`; `mention_service.go:46`, `50` | Rechazar un nombre (o una lista de menciones) que tiene **exactamente** el máximo permitido. Había tests con valores muy por encima del máximo, ninguno justo en el borde. | `TestCreateRole_NameAtMaxLength_Allowed`, `TestUpdateRole_NameAtMaxLength_Allowed`, `TestCreateCategory_NameLength_MaxIsAllowed`, `TestCreateChannel_NameLength_MaxIsAllowed`, `TestResolve_MentionsAtTheCap_AreAllowed` |
| `role_service.go:220` (3 mutantes) | Dejar renombrar o cambiarle el color a `@everyone`. No había ningún test sobre `@everyone`. | `TestUpdateRole_Everyone_OnlyPermissionsCanChange` |
| `moderation_service.go:282` | Al silenciar, no volver a revisar el permiso del moderador dentro de la transacción (si lo perdió en el medio, el mute igual se aplicaba). Ban y kick ya tenían este test; mute no. | `TestMuteMember_RechecksInsideTheTransaction` |
| `server_service.go:329` (2 mutantes) | Al renombrar un servidor, **borrar su banner**; o al subir un banner nuevo, dejar el viejo guardado. | `TestUpdateServer_NameOnlyChange_DoesNotDeleteTheBanner`, `TestUpdateServer_BannerChange_DeletesThePreviousVersion` |
| `server_service.go:198`, `215` | En la lista de servidores, cortar el armado después de un servidor suspendido: los siguientes quedaban sin filtrar los canales ocultos. El test que existía usaba un servidor propio (el dueño ve todo), así que no se notaba. | `TestListMyServers_ServerAfterASuspendedOneIsStillFiltered` |
| `role_service.go:299`, `418`; `category_service.go:165`; `channel_service.go:339` | Tragarse el error del repositorio y responder OK. | `TestSetDefaultRole_RepoError_Propagates`, `TestDeleteRole_RepoError_Propagates`, `TestReorderCategories_RepoError_Propagates`, `TestDeleteChannel_RepoError_Propagates` |
| `invitation/service.go:90` | Probar 6 códigos de invitación en vez de 5 antes de rendirse. El test verificaba que se rinde, no cuántas veces intentó. | Se agregó la verificación a `TestGenerate_GivesUpAfterTooManyCollisions` |

En los límites de largo usamos `á` en vez de `a`: así el test también verifica
que se cuentan **caracteres** y no bytes (en UTF-8, cada `á` ocupa 2 bytes).

**9 son mutantes equivalentes**: el cambio no altera nada que se pueda
comprobar.

| Dónde | Por qué es equivalente |
|---|---|
| `server_service.go:482`, `emoji_service.go:104`, `ownership_transfer_service/expirer.go:49`, `53` (2) | Solo cambian cuándo se escribe un log |
| `server_service.go:187` | Con error, el repositorio devuelve una lista vacía, así que `\|\|` y `&&` dan lo mismo |
| `report_service.go:187` | Los ids de canal son únicos: `break` y `continue` encuentran el mismo canal |
| `invitation/service.go:102` | El `continue` está dentro de un `switch`: en Go, un `break` ahí sale del `switch` y no del `for`, así que el bucle sigue igual |
| `ownership_transfer_service.go:168` | Cambia `<=` por `<` en el vencimiento de 48 h: la diferencia es un solo nanosegundo exacto. El reloj del servicio no se puede fijar desde los tests, y agregar eso solo para este caso no vale la pena |

### Lo que no cuenta

- **Los 3 timeouts de la primera corrida** (`authz.go:38` y `48`): en la
  segunda corrida esos mutantes mueren de forma normal, así que no escondían
  ningún sobreviviente.
- **Los no cubiertos** son en su mayoría constantes, que la cobertura de Go
  no mide, y condiciones de `case` en un `switch` cuyo cuerpo sí está cubierto.
