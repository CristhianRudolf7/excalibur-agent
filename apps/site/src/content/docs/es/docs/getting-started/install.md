---
title: 'Instalar Excalibur'
description: 'Compila la aplicación de terminal y conoce el estado de cada plataforma.'
---

Necesitas una copia del repositorio de Excalibur y la versión de Go indicada en `go.mod`. Estas instrucciones compilan la aplicación de línea de comandos desde el código fuente. No instalan ni abren la aplicación web.

## Disponibilidad por plataforma

| Plataforma | Estado actual                                                          | Inicio automático opcional                    |
| ---------- | ---------------------------------------------------------------------- | --------------------------------------------- |
| Linux      | Plataforma principal compatible                                        | Servicio de usuario de systemd                |
| macOS      | Compatibilidad en desarrollo, compilaciones para Apple Silicon e Intel | LaunchAgent de usuario de launchd             |
| Windows    | Compatibilidad en desarrollo                                           | Tarea por usuario en el Programador de tareas |

Hay integraciones nativas para macOS y Windows, pero la compatibilidad sigue en desarrollo. Una compilación correcta no demuestra que todas las funciones operen en esa plataforma. La navegación automatizada necesita un navegador compatible instalado por separado. Las terminales interactivas de Windows requieren Windows 10 versión 1809 o posterior.

## Compila el ejecutable

Desde la raíz del repositorio, en Linux o macOS y con un compilador de C instalado:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o excalibur ./cmd/excalibur
./excalibur
```

En macOS, el compilador y el SDK se obtienen con las herramientas de línea de comandos de Apple. La compilación `sqlite_fts5` incorpora las extensiones nativas de búsqueda de SQLite. La compilación normal de Go usa la base de datos portable y no incluye la búsqueda vectorial de sqlite-vec.

En PowerShell de Windows:

```powershell
go build -o excalibur.exe ./cmd/excalibur
.\excalibur.exe
```

Al ejecutar el programa sin argumentos verás los comandos disponibles. Guarda el ejecutable en una ubicación estable y agrega su directorio a `PATH` si quieres usar `excalibur` desde cualquier terminal.

## Prepara la instalación de usuario

Ejecuta el programa con `install`, por ejemplo `./excalibur install` en Linux o macOS. Crea los directorios y la configuración del usuario e intenta instalar el controlador de automatización del navegador. Ese paso puede descargar el controlador, pero no descarga Chrome. Revisa las advertencias del informe.

El inicio automático es opcional. Después de [configurar un proveedor](/es/docs/getting-started/providers/), completa el [inicio rápido](/es/docs/getting-started/quick-start/). Instalar el daemon no conecta una cuenta de modelos ni proporciona credenciales.
