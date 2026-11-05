---
title: 'Extensiones y tienda'
description: 'Distingue las aplicaciones incluidas, las extensiones locales y los límites de la tienda.'
---

Abre Extensiones en la aplicación web para encontrar Aula, Laboratorio de robótica y la entrada del brazo robótico. Sus controles de instalación o eliminación administran qué entradas aparecen en la navegación local. No descargan un paquete remoto ni instalan las dependencias de Python del laboratorio.

## Elige una aplicación incluida

Por ejemplo, abre la entrada de Aula, habilítala en la navegación y después [crea un curso](/es/docs/extensions/classroom/). Cada aplicación tiene sus propios requisitos. El Laboratorio de robótica necesita un paso separado y explícito para instalar dependencias, explicado en [su guía](/es/docs/extensions/robotics-lab/).

El catálogo incluido es pequeño y forma parte de la aplicación. Esta versión no dispone de un servicio remoto de tienda en producción ni de un catálogo publicado completo. Que un control parezca una tienda no demuestra que permita descargar herramientas o flujos remotos.

## Consulta las extensiones locales

La aplicación de terminal tiene un administrador separado:

```sh
excalibur ext list
```

Muestra las extensiones del directorio configurado. La familia `ext` también permite crear una estructura inicial, instalar desde un directorio local, habilitar, deshabilitar, eliminar, consultar registros y revisar el estado. Usa `excalibur ext --help` para ver los subcomandos. Algunos no tienen ayuda propia con `--help`; no lo agregues a una instalación suponiendo que siempre será una consulta sin efectos.

El directorio de una extensión debe contener su manifiesto y los archivos necesarios. Instalarlo es diferente de habilitar una entrada del catálogo web. Comprueba las plataformas compatibles y los programas requeridos antes de usarlo.

Solicita una instalación solo cuando quieras agregar esa extensión. Que figure en una lista no garantiza que pueda ejecutarse si falta un programa o servicio. Consulta la [referencia de CLI](/es/docs/reference/cli/) y la guía de [seguridad](/es/docs/reference/security/) para conocer los controles relacionados.
