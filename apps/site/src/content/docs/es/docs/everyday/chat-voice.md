---
title: 'Chat y voz en vivo'
description: 'Conversa por texto o habla con el agente seleccionado.'
---

Abre la conversación del agente que quieres usar. Cada agente personal tiene su propio historial. Al elegir otro agente, los mensajes nuevos y los turnos de voz se envían a su conversación. Para recibir respuestas necesitas el daemon activo y un proveedor de modelos configurado.

## Haz una solicitud

Explica el resultado que esperas, el material que debe usar y los límites de la tarea. Por ejemplo:

> Lee las notas de la reunión que adjunté y prepara una lista breve de preguntas pendientes. No envíes nada.

Adjunta las notas, espera a que termine su procesamiento y envía el mensaje. Los documentos pueden servir como fuente para la respuesta. Consulta [Documentos y citas](/es/docs/everyday/documents/) cuando necesites comprobar un pasaje.

También puedes enviar un mensaje desde la terminal:

```sh
excalibur chat "Help me organize the next steps for this project."
```

Este comando envía un mensaje al daemon. No abre una ventana de chat interactiva en la terminal.

## Habla en lugar de escribir

Usa los controles de voz de la conversación y permite el acceso al micrófono cuando el navegador lo solicite. Las llamadas pueden transcribir lo que dices y reproducir las respuestas. El panel de llamada permite controlar el micrófono, el altavoz y el fin de la llamada. Al terminar, libera el micrófono usado por esa llamada.

La voz depende de las funciones de audio del navegador y de los servicios configurados. Excalibur dispone de rutas de voz en tiempo real, de servidor y del navegador, con alternativas cuando un servicio falla. Su disponibilidad y calidad pueden variar. La voz del navegador puede usar servicios de su fabricante; no supongas que todo se procesa localmente.

Si el navegador rechaza el micrófono, habilítalo en su configuración o continúa por texto. Corrige una transcripción incorrecta antes de pedir una acción externa. Una solicitud por voz mantiene los [requisitos de confirmación](/es/docs/reference/security/), incluido el envío de Gmail.
