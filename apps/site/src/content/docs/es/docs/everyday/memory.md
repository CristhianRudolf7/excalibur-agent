---
title: 'Memoria'
description: 'Guarda preferencias útiles y recupera información de trabajos anteriores.'
---

Usa la memoria para información que conviene conservar más allá del mensaje actual: una preferencia, una restricción habitual o un dato que quieres volver a consultar. La memoria es distinta de la biblioteca de documentos y del historial completo de conversaciones.

## Expresa una preferencia con claridad

Por ejemplo:

> Recuerda que prefiero las fechas en formato año-mes-día en los informes.

Excalibur tiene herramientas para guardar hechos y preferencias, y para buscar recuerdos. También dispone de una búsqueda separada en mensajes y acciones anteriores. Pregunta qué recuerda de un tema cuando quieras revisar la información que está usando.

Los recuerdos pueden tener un ámbito y fechas de validez. Una preferencia general y un dato de un proyecto no necesariamente corresponden a los mismos contextos. Si algo cambia, explica qué cambió y desde cuándo. La información guardada puede actualizarse o sustituirse; no supongas que una frase nueva borra todas las menciones anteriores del historial.

## Consulta desde la terminal

Con el daemon activo, puedes buscar en la memoria general:

```sh
excalibur memory "report date format"
```

El ámbito predeterminado es `general`. El comando también acepta `--scopes` para indicar varios ámbitos separados por comas. Devuelve el resultado de la consulta del daemon; no inicia una respuesta conversacional nueva.

Los agentes personales comparten la memoria del usuario aunque sus conversaciones estén separadas. Cambiar de agente no crea una partición privada de recuerdos. Lee [Agentes y subagentes](/es/docs/building/agents/) antes de organizar el trabajo con varias personalidades.

La recuperación permite encontrar material pertinente, pero no garantiza que cada afirmación anterior aparezca en todas las respuestas. Comprueba los datos importantes en su fuente original. Usa [Documentos y citas](/es/docs/everyday/documents/) para consultar archivos y la [bóveda de contraseñas](/es/docs/everyday/browser-passwords/) para credenciales, en vez de guardarlas como recuerdos normales.
