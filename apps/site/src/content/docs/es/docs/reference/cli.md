---
title: 'Referencia de CLI'
description: 'Consulta comandos, controla tareas e interpreta su estado de salida.'
---

Ejecuta `excalibur` sin argumentos para listar los comandos. En esta versión, `excalibur --help` solo muestra la ayuda de la opción global. Familias como `excalibur task --help` enumeran sus subcomandos. Algunas consultas de ayuda imprimen el texto y terminan con un error; su código de salida no sirve para comprobar la salud del daemon.

## Comandos habituales

| Comando                    | Función                                  |
| -------------------------- | ---------------------------------------- |
| `excalibur daemon start`   | Ejecutar el daemon                       |
| `excalibur daemon stop`    | Solicitar que se detenga                 |
| `excalibur chat "MESSAGE"` | Enviar un mensaje                        |
| `excalibur exec "REQUEST"` | Ejecutar una solicitud no interactiva    |
| `excalibur status`         | Consultar ejecuciones mediante el daemon |
| `excalibur tasks`          | Listar tareas mediante el daemon         |
| `excalibur projects`       | Listar proyectos registrados             |
| `excalibur memory "QUERY"` | Consultar memoria                        |
| `excalibur skills "QUERY"` | Buscar habilidades                       |
| `excalibur ext --help`     | Listar subcomandos de extensiones        |
| `excalibur vault --help`   | Listar subcomandos de la bóveda          |

El texto en mayúsculas y los identificadores de ejemplo son marcadores. Sustitúyelos por tu solicitud o por el identificador recibido. Chat, estado, tareas, proyectos, memoria y habilidades necesitan el daemon. `exec` puede funcionar sin él, pero requiere un proveedor configurado.

## Sigue una tarea en segundo plano

```sh
excalibur task start "Review the README without editing files."
excalibur task status TASK_ID
excalibur task wait TASK_ID --timeout 300
excalibur task result TASK_ID --format md
```

`task start` devuelve un identificador de inmediato. Si hay una pregunta pendiente, responde con `excalibur task reply TASK_ID "ANSWER"`. La familia incluye `pause`, `resume`, `cancel`, `list` y `logs`. Mantén el mismo directorio `--project` al consultar una tarea guardada en el estado del proyecto.

La opción global `--json` va antes del comando, por ejemplo `excalibur --json status`. `exec` también acepta `--json` para emitir eventos y un resumen final en formato procesable.

## Interpreta la finalización

Para `exec` y los estados terminales que devuelve `task wait`:

| Código | Significado                                 |
| ------ | ------------------------------------------- |
| 0      | Completado                                  |
| 1      | Error de comando, configuración o ejecución |
| 2      | Completado con pendientes                   |
| 3      | Bloqueado                                   |
| 4      | Pausado o con tiempo de ejecución agotado   |
| 5      | Fallido                                     |
| 130    | Cancelado                                   |

Una espera limitada puede terminar mientras el trabajo sigue activo. Lee el estado además del código. Consulta el [inicio rápido](/es/docs/getting-started/quick-start/) y la [configuración](/es/docs/reference/configuration/) antes de comenzar.
