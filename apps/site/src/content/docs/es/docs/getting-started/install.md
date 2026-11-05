---
title: 'Instalar Excalibur'
description: 'Instala la interfaz y el backend juntos, abre Excalibur y gestiona las actualizaciones.'
---

La instalación de una versión publicada incluye la interfaz y el backend. Elige el comando para tu sistema operativo y abre **http://127.0.0.1:7787** en el mismo equipo. Antes de pedir trabajo al asistente, debes [conectar un proveedor de modelos](/es/docs/getting-started/providers/).

## Linux y macOS

Ejecuta en una terminal:

```sh
curl -fsSL https://excalibur.crehlabs.com/install.sh | sh
```

El instalador descarga la versión para tu plataforma y prepara la instalación de usuario. Revisa la ubicación del ejecutable y las advertencias que aparezcan en su salida. Si el comando todavía no está en tu `PATH`, abre una terminal nueva.

## Windows

Ejecuta en PowerShell:

```powershell
irm https://excalibur.crehlabs.com/install.ps1 | iex
```

La compatibilidad con Windows y macOS sigue en desarrollo. Existen integraciones específicas, pero no todas las funciones se han verificado en equipos nativos. La automatización necesita un navegador compatible; las terminales interactivas de Windows requieren Windows 10 versión 1809 o posterior.

## Docker

Con Docker en ejecución:

```sh
docker run -d --name excalibur -p 127.0.0.1:7787:7787 -v excalibur-data:/data ghcr.io/cristhianrudolf7/excalibur:latest
```

El puerto solo escucha en la dirección local del equipo. El volumen conserva los datos al reemplazar el contenedor. Los navegadores y dispositivos del anfitrión no están disponibles dentro del contenedor automáticamente; esos flujos requieren configuración adicional.

## Actualizar

Para una instalación de una versión publicada:

```sh
excalibur update
```

En Docker, descarga la imagen nueva, detén y elimina el contenedor anterior y repite el comando de instalación. Conserva el volumen:

```sh
docker pull ghcr.io/cristhianrudolf7/excalibur:latest
docker stop excalibur
docker rm excalibur
```

## Desinstalar

Primero detén Excalibur y desactiva cualquier inicio automático que hayas habilitado: systemd en Linux, launchd en macOS o el Programador de tareas en Windows. Elimina el ejecutable de la ubicación indicada por el instalador. Conserva los directorios de configuración y datos si quieres mantener conversaciones y credenciales. No existe el comando `excalibur uninstall`.

En Docker, detén y elimina el contenedor como se muestra arriba. Eliminar el volumen `excalibur-data` es un paso aparte e irreversible: borra sus datos guardados.

## Compilar desde el código fuente

Con Go y un compilador de C instalados, ejecuta desde la raíz del repositorio:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o excalibur ./cmd/excalibur
```

Esto solo compila el ejecutable. La compilación normal de Go usa la base de datos portable sin sqlite-vec. Continúa con el [inicio rápido](/es/docs/getting-started/quick-start/).
