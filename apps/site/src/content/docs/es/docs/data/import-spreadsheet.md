---
title: 'Importar una hoja de cálculo'
description: 'Revisa los campos y resuelve valores ambiguos antes de guardar las filas.'
---

Usa Importar en Mis datos para convertir un archivo CSV o Excel en registros. Necesitas la aplicación web conectada al daemon y un agente disponible. El selector admite `.csv`, `.xlsx` y `.xls`; los formatos antiguos de Excel también dependen del soporte de conversión de documentos de la instalación.

## Revisa antes de guardar

1. Selecciona Importar desde Mis datos o desde una colección existente.
2. Elige el archivo. Excalibur lo carga y prepara una solicitud de conversación con el adjunto.
3. Envía la solicitud y pide una vista previa. Revisa campos propuestos, correspondencias, correcciones y conflictos.
4. Si necesitas una colección nueva, confirma su estructura y pide aplicar las filas válidas.

`Datos de ejemplo`

```csv
title,author,status
Example book A,Example author A,To read
Example book B,Example author B,Reading
```

El ejemplo contiene dos registros y tres campos. Sirve para mostrar la estructura de una importación; no representa tu lista de lectura real.

## Aclara los valores dudosos

Las importaciones comienzan como una simulación que no guarda registros. La vista previa puede inferir campos y detectar conflictos sin crear datos. Las correcciones automáticas pueden quitar espacios, normalizar comas decimales y opciones, y convertir fechas inequívocas. Revisa qué se corrigió antes de aplicar el resultado.

Una fecha ambigua o un valor obligatorio faltante debe quedar pendiente, sin inventar información. Al aclararlo, indica la fila, la columna original y el valor correcto. Puedes importar las filas válidas y resolver las demás después.

Si vuelves a usar el mismo archivo, las filas idénticas ya importadas se omiten como duplicadas. Esto no significa que cualquier archivo modificado reemplace registros de forma segura. Revisa otra vez la vista previa y las filas pendientes.

Al terminar, abre la colección y compara los registros con el informe. Más adelante puedes [exportarla](/es/docs/data/interfaces/). Si solo necesitas leer la hoja, usa la [biblioteca de documentos](/es/docs/everyday/documents/) sin convertirla en colección.
