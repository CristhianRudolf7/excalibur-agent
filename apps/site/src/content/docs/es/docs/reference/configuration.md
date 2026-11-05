---
title: 'Configuración'
description: 'Encuentra los archivos y ajusta modelos, voz y herramientas.'
---

Excalibur lee configuración en TOML. Empieza con el archivo de usuario y agrega opciones de proyecto solo si necesitas un comportamiento diferente en ese proyecto. No guardes valores de credenciales en archivos que se incluyan en el control de versiones.

## Encuentra el archivo de usuario

| Plataforma    | Archivo predeterminado            |
| ------------- | --------------------------------- |
| Linux y macOS | `~/.config/excalibur/config.toml` |
| Windows       | `%APPDATA%\Excalibur\config.toml` |

En Linux y macOS, `XDG_CONFIG_HOME` reemplaza a `~/.config` cuando está definido. El archivo del proyecto es `.excalibur/config.toml`; Excalibur lo busca ascendiendo desde el directorio inicial.

Las opciones se combinan en este orden: valores predeterminados, sistema, usuario, proyecto y archivo de ámbito indicado explícitamente. Después se aplican las variables de entorno y, cuando el comando las admite, las opciones de línea de comandos. Las tablas se combinan; los valores simples y los arreglos sustituyen a los anteriores.

## Configura un modelo

El ejemplo usa un servidor Ollama local instalado por separado. Cambia el marcador por un modelo disponible en ese servidor:

```toml
[models]
provider = "ollama"
model = "YOUR_MODEL_ID"

[tools]
default_timeout = "60s"
```

Ese límite corresponde a las herramientas, no al tiempo total de una solicitud. Cada comando o herramienta puede tener sus propios límites.

Otras claves del modelo son `base_url`, `api_key_env`, `api_key_secret` y `credentials_file`. Si el daemon dispone de la bóveda, `api_key_secret` puede apuntar a una entrada existente como `secret://model-key`. Escribir la referencia no crea ni desbloquea el secreto.

## Sobrescribe una opción

La forma general de una variable es `EXCALIBUR_SECTION__FIELD`, por ejemplo `EXCALIBUR_TOOLS__DEFAULT_TIMEOUT=60s`. Para modelos existen `EXCALIBUR_PROVIDER`, `EXCALIBUR_MODEL` y `EXCALIBUR_BASE_URL`. La variable habitual de clave de API del proveedor también puede determinar la credencial.

Las opciones de voz están en `[voice]`, incluidas `enabled`, `provider`, `stt_model`, `tts_model` y `voice`. El acceso al robot físico tiene su propia opción `enabled` en `[robot]`. Estas opciones no instalan servicios adicionales.

Reinicia el daemon tras cambiar su configuración inicial. Si un valor parece no aplicarse, revisa el proyecto y el entorno. Consulta [Conectar un proveedor](/es/docs/getting-started/providers/) y [Seguridad y permisos](/es/docs/reference/security/) antes de agregar credenciales.
