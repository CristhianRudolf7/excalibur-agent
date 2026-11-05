---
title: 'Vista previa pública con Cloudflare'
description: 'Comparte un enlace temporal de una web compilada o un servidor local.'
---

Pide una vista previa pública para abrir una web en tu teléfono o compartirla. Excalibur usa Cloudflare Quick Tunnels. El equipo donde se ejecuta el daemon necesita `cloudflared` en `PATH`, o su ruta configurada mediante `EXCALIBUR_CLOUDFLARED`.

## Solicita un enlace público

Después de compilar el sitio, puedes pedir:

> Publica una vista previa temporal de este sitio para abrirlo en mi teléfono.

Solicitar expresamente un enlace público o de Cloudflare autoriza la publicación. Excalibur no necesita otra confirmación para esa solicitud. Si no hay una petición explícita de publicación, la herramienta rechaza la operación.

Puede servir un directorio estático compilado que esté dentro de un proyecto o espacio de trabajo permitido. También puede exponer un servidor HTTP existente en la interfaz local de loopback. Un directorio de código fuente que requiere compilación no es automáticamente una web lista para servir. Pide que lo compile primero.

## Comprueba el enlace recibido

Una publicación correcta devuelve una URL pública real y su vencimiento. La conversación puede mostrarlos en una tarjeta. No des por hecho que el enlace existe solo porque el asistente anunció que iniciaría un túnel. Si falla, corrige el problema indicado y vuelve a intentarlo.

Cualquier persona que tenga el enlace puede ver el contenido publicado. Elige con cuidado el directorio o servidor y evita publicar documentos privados o páginas de desarrollo con credenciales. La vista previa no es una sala de revisión con autenticación.

La duración predeterminada es de dos horas y puedes solicitar hasta 24 horas. También termina cuando se detiene el daemon. Puedes pedir que liste las vistas activas y cierre una antes de su vencimiento.

Cerrar la vista previa detiene el túnel y el servidor que Excalibur haya creado para ella. No detiene un servidor HTTP que hayas iniciado por separado. Consulta [Programación](/es/docs/building/coding/) para crear el sitio.
