---
title: 'Laboratorio de robótica'
description: 'Instala el entorno opcional de simulación y revisa una sesión.'
---

El Laboratorio de robótica ejecuta simulaciones. Es distinto de la interfaz del brazo robótico físico. Un movimiento simulado no mueve hardware conectado y un resultado correcto en simulación no demuestra que sea seguro en un robot real.

## Prepara el entorno

Abre el laboratorio y consulta su estado de instalación. Necesita Python 3.10 o posterior y dependencias opcionales de Python. En Linux también requiere OSMesa para renderizar sin pantalla. macOS usa CGL integrado y necesita Apple Silicon; las dependencias fijadas no admiten equipos Intel.

La compatibilidad con Windows y macOS sigue en desarrollo. El soporte del entorno y la disponibilidad de una herramienta en el perfil del agente pueden ser distintos. Guíate por el estado de tu instalación, sin asumir que todas las plataformas tienen las mismas funciones operativas.

Selecciona Instalar de forma explícita para obtener las dependencias opcionales. Abrir la página de estado no las instala automáticamente. Revisa el progreso y los errores. Tener la entrada en la navegación no significa que el entorno esté listo.

## Inicia una simulación pequeña

Cuando esté listo y haya un agente conectado, pide un objetivo sencillo. Por ejemplo:

> Inicia una simulación del brazo en un entorno de mesa. Revisa el robot y la escena antes de intentar moverlo.

Los robots simulados incluyen un brazo y un cuadrúpedo. Las sesiones usan un entorno y muestran observaciones, estado del robot, actividad y registros. Las cámaras y la vista de escena ayudan a inspeccionarlo; revisa también el estado y el resultado del objetivo.

## Revisa y detén la sesión

Si un movimiento tiene un resultado incierto, consulta el estado actual. No deduzcas el éxito solo por la postura. Puedes detener una sesión y después pedir que retome una anterior pertinente.

Los entornos se editan mediante solicitudes de texto. Indica dimensiones concretas y aclara las medidas ambiguas antes de ejecutar la escena. Para otras aplicaciones opcionales, vuelve a [Extensiones](/es/docs/extensions/overview/).
