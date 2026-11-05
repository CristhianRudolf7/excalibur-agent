---
title: 'Programación'
description: 'Encarga una tarea de proyecto y revisa comandos, cambios y resultados.'
---

Excalibur puede revisar archivos, buscar en un proyecto, modificar código y ejecutar comandos de terminal. Trabaja desde el directorio del proyecto y conecta un proveedor de modelos. Explica el problema, el comportamiento esperado y los archivos o comandos que debe evitar.

## Delimita la solicitud

Por ejemplo:

```sh
excalibur exec "Find the cause of the failing parser test. Make the smallest fix and run the relevant tests. Do not commit changes."
```

Esta solicitud autoriza modificaciones y pruebas. Si solo quieres una revisión, indícalo expresamente. `exec` no se detiene para hacer preguntas, así que incluye desde el principio las restricciones que importan.

## Revisa la actividad en el chat

Durante el trabajo de programación, la conversación puede mostrar ejecuciones y subagentes, pasos del plan, lecturas de archivos, comandos, modificaciones y resultados de pruebas. Expande las entradas para consultar su salida o sus diferencias cuando estén disponibles. Los resultados truncados y los archivos binarios tienen presentaciones limitadas.

El panel de archivos puede abrir archivos del espacio de trabajo y mostrar líneas modificadas o eliminadas, con navegación entre cambios. Parte del contenido alternativo del editor actual es de ejemplo. Un proyecto de muestra o una etiqueta general de estado no demuestra nada sobre tu repositorio.

El editor del navegador también conserva las modificaciones manuales en el estado local de la interfaz. No supongas que escribir allí guarda el archivo en disco. Pide al agente que aplique el cambio y después verifica el archivo real o sus diferencias.

## Comprueba la entrega

Lee el resultado final y la salida de los comandos. Una prueba correcta solo respalda las comprobaciones que se ejecutaron. Si el trabajo falló o se detuvo, consulta su estado antes de iniciar algo que dependa de él.

Usa [tareas en segundo plano](/es/docs/getting-started/quick-start/) si quieres revisar el trabajo más tarde. Para abrir una web en otro dispositivo, pide una [vista previa pública](/es/docs/building/public-preview/) después de compilarla.
