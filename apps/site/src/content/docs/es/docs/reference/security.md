---
title: 'Seguridad y permisos'
description: 'Protege tus credenciales y conoce los límites de las confirmaciones.'
---

Excalibur puede leer archivos, ejecutar comandos, cambiar datos locales y actuar en servicios conectados. Dale acceso solo a los proyectos y cuentas que quieras usar. Una instrucción en la conversación delimita el trabajo, pero no sustituye los permisos del sistema operativo ni un entorno aislado.

## No envíes credenciales como mensajes

Usa los formularios de credenciales para accesos web y códigos de un solo uso. Para claves de API, utiliza variables de entorno del proveedor o referencias a la bóveda. Un mensaje normal de chat no es un formulario de secretos.

La bóveda cifra los valores y permite bloquearla, desbloquearla y restringir entradas por ámbito, herramientas y dominios. Las listas muestran metadatos, no valores. Consulta los controles de terminal con:

```sh
excalibur vault --help
```

`set` recibe el valor mediante entrada oculta de terminal o `--stdin`, no como argumento de valor. `get` revela deliberadamente un secreto; evita usarlo en grabaciones o terminales compartidas. Consulta [Navegador y bóveda de contraseñas](/es/docs/everyday/browser-passwords/) para los formularios web.

## Revisa las acciones externas

Enviar Gmail, mover mensajes a la papelera y eliminarlos requieren aprobación explícita en el chat. También la necesitan compartir, enviar a la papelera o eliminar en Drive y reemplazar texto en Documentos de Google. Otras operaciones, incluidas algunas escrituras, siguen reglas distintas. No supongas que todo cambio externo espera un diálogo de aprobación.

Pedir una vista previa pública autoriza su publicación. Cualquiera con el enlace puede ver el contenido. Revisa los archivos antes de solicitarlo y cierra la vista previa cuando ya no haga falta.

## Entiende qué se comparte

Los agentes personales tienen conversaciones separadas, pero comparten memoria y recursos. No aíslan cuentas. Una solicitud de terminal puede modificar archivos y ejecutar comandos; `exec` no se detiene a hacer preguntas.

Las protecciones varían entre sistemas y la compatibilidad con Windows y macOS sigue en desarrollo. Estos controles no vuelven seguras todas las extensiones ni todos los comandos. Revisa sus requisitos y los cambios reales cuando el resultado importe. Consulta [Configuración](/es/docs/reference/configuration/) y [Herramientas de Google](/es/docs/everyday/google/) para más detalles.
