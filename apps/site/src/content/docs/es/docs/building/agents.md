---
title: 'Agentes y subagentes'
description: 'Crea un agente personal y conoce cómo se delega el trabajo.'
---

Los agentes personales permiten mantener conversaciones y personalidades distintas. Los subagentes realizan partes de una tarea delegada. Cumplen funciones diferentes: un agente personal permanece en tu lista, mientras que un subagente trabaja dentro de una ejecución.

## Crea un agente personal

En la configuración de agentes, elige la opción para crear uno. Se abrirá la conversación del agente favorito con una solicitud preparada. Revísala y envíala; abrir el chat no crea el agente por sí solo.

Por ejemplo:

> Crea un agente personal llamado Compañero de escritura. Quiero que me ayude a revisar explicaciones técnicas y pregunte por el público antes de proponer cambios.

Después podrás elegirlo en el selector. La configuración permite modificar nombre, título, descripción y personalidad, establecerlo como favorito, archivarlo o restaurarlo. Las solicitudes de avatar también se envían por conversación.

Cada agente personal tiene su propio historial, incluidos los turnos de voz. Todos comparten la memoria y los recursos del usuario. No son cuentas separadas ni límites de seguridad. Archivar conserva el historial y retira al agente de los selectores activos. No puedes archivar al último agente activo.

## Delega una tarea

Al programar, explica el objetivo, los límites y el resultado esperado. El sistema de tareas puede ejecutar trabajo delegado y subagentes. Una ejecución principal coordina a sus trabajadores; no hace falta crear un agente personal permanente para cada revisión.

Desde la terminal puedes iniciar una revisión limitada:

```sh
excalibur task start "Review this project's README and report unclear setup instructions without editing files." --max-steps 20
```

Recibirás un identificador. Consulta su estado y resultado con la [referencia de CLI](/es/docs/reference/cli/). Una tarea puede necesitar una respuesta o detenerse al alcanzar un límite. Que siga activa no significa que el objetivo esté cumplido.

Crea un agente personal para separar conversaciones. Pide delegación para dividir un mismo trabajo. Consulta [Programación](/es/docs/building/coding/) para revisar comandos y cambios de archivos.
