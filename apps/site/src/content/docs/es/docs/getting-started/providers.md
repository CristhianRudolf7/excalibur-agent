---
title: 'Conectar un proveedor de modelos'
description: 'Elige un proveedor y prepara sus credenciales para Excalibur.'
---

Excalibur necesita un proveedor de modelos para responder solicitudes. Ten a mano el identificador del modelo que vas a usar. Un servicio alojado también requiere credenciales de API. Si prefieres Ollama local, instala e inicia ese servidor por separado y verifica que tenga disponible el modelo elegido.

## Prueba un proveedor local

Agrega esta tabla al archivo de usuario `config.toml` y sustituye el marcador del modelo:

```toml
[models]
provider = "ollama"
model = "YOUR_MODEL_ID"
```

En Linux y macOS, la ruta predeterminada es `~/.config/excalibur/config.toml`, o la equivalente dentro de `XDG_CONFIG_HOME`. En Windows se encuentra en `%APPDATA%\Excalibur`. Ollama usa su dirección local predeterminada salvo que definas `base_url`. Su adaptador no necesita una clave de API.

Guarda el archivo y envía la primera solicitud del [inicio rápido](/es/docs/getting-started/quick-start/). Si cambias opciones que usa un daemon activo, reinícialo para que las tome.

## Conecta un servicio alojado

Los adaptadores de API incluyen `anthropic`, `gemini`, `openai`, `deepseek` y `openrouter`. Usa el identificador del proveedor y un modelo real disponible en tu cuenta. El nombre del proveedor no elige un modelo por ti.

Las variables habituales de credenciales son:

| Proveedor  | Variable de credenciales            |
| ---------- | ----------------------------------- |
| Anthropic  | `ANTHROPIC_API_KEY`                 |
| Gemini     | `GEMINI_API_KEY` o `GOOGLE_API_KEY` |
| OpenAI     | `OPENAI_API_KEY`                    |
| DeepSeek   | `DEEPSEEK_API_KEY`                  |
| OpenRouter | `OPENROUTER_API_KEY`                |

Configura la credencial mediante las opciones de secretos de tu terminal o servicio. No la pegues en el chat ni la guardes en un archivo versionado del proyecto. El proceso de Excalibur debe recibir esa variable.

Un servicio compatible puede usar `generic` con un `base_url` explícito. También existe el inicio de sesión en Configuración → Conexiones → Cuenta de ChatGPT / Codex. Conectar la cuenta y seleccionar el modelo son pasos distintos. Consulta [Configuración](/es/docs/reference/configuration/) para conocer las variables de entorno y referencias a la bóveda.
