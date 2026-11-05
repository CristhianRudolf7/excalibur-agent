---
title: 'Inicio rápido'
description: 'Envía tu primera solicitud y consulta una tarea en segundo plano.'
---

Antes de comenzar, [instala el ejecutable](/es/docs/getting-started/install/) y [conecta un proveedor de modelos](/es/docs/getting-started/providers/). Los ejemplos suponen que `excalibur` está en `PATH`. Envía solicitudes sobre proyectos desde un directorio que Excalibur tenga permitido leer y modificar.

## Envía una primera solicitud

Para una solicitud no interactiva:

```sh
excalibur exec "List the top-level files in this directory and explain their purpose."
```

`exec` realiza el trabajo e imprime un resumen final. Puede usar herramientas y modificar archivos si la solicitud lo requiere. No se detiene a hacer preguntas, así que incluye tus restricciones desde el principio. Si no hay un proveedor configurado, termina con un error en lugar de dar una respuesta simulada.

Para mantener una conversación, inicia el daemon en otra terminal:

```sh
excalibur daemon start
```

Después envía un mensaje desde una segunda terminal:

```sh
excalibur chat "Help me plan a small documentation update."
```

El comando de conversación necesita conectarse al daemon. Iniciarlo no abre automáticamente el navegador ni la aplicación web.

## Deja una tarea en segundo plano

Este comando devuelve un identificador de inmediato:

```sh
excalibur task start "Review the README for broken relative links. Report findings without editing files."
```

Copia el identificador recibido y úsalo con `excalibur task status TASK_ID`, `excalibur task wait TASK_ID` y `excalibur task result TASK_ID`. Sustituye `TASK_ID` por el valor real; es un marcador de ejemplo.

Una tarea puede necesitar una respuesta, quedar bloqueada o fallar. Revisa su estado y su resultado antes de dar el trabajo por terminado. La [referencia de CLI](/es/docs/reference/cli/) explica cómo responder, cancelar e interpretar los códigos de salida.

Si una solicitud no logra conectarse al modelo, comprueba el proveedor, el identificador del modelo y el origen de las credenciales. Deben estar disponibles para el proceso que ejecuta la solicitud, incluido el daemon cuando usas el chat.
