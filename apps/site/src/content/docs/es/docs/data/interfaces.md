---
title: 'Vistas y formularios de colecciones'
description: 'Busca, filtra, edita y exporta los registros de una colección.'
---

Cada colección ya tiene una interfaz en Mis datos: una tabla, páginas de detalle y formularios basados en sus campos. No necesitas generar otra aplicación para consultar esos registros. Parte de una colección existente o [crea una en la conversación](/es/docs/data/my-data/).

## Encuentra el registro que necesitas

Abre la colección y usa la búsqueda para reducir la lista. Los filtros permiten consultas más concretas, como mostrar los libros cuyo estado sea “Leyendo”. Puedes cambiar el orden y ocultar columnas que no necesites ver en la tabla.

Por ejemplo, después de crear una colección de libros:

1. Abre su lista de registros.
2. Filtra el campo de estado por la opción “Leyendo”.
3. Abre un resultado y revisa su detalle.
4. Modifica el registro mediante su formulario y guarda el cambio.

Usa los valores y opciones que realmente existan en tu colección. La etiqueta de este ejemplo no agrega una opción nueva a la estructura.

## Usa formularios y relaciones

Los formularios siguen los tipos y las restricciones de los campos. Las fechas, opciones, números y relaciones requieren entradas diferentes. Una relación puede abrir el registro vinculado y el detalle puede mostrar registros relacionados de otras colecciones.

La interfaz web pide confirmar la eliminación de un registro. Este paso no debe confundirse con las reglas de la herramienta del asistente: la herramienta puede modificar registros individuales sin una aprobación adicional.

## Exporta la selección

La lista permite exportar CSV o XLSX con los filtros activos. Revisa los filtros si quieres descargar la colección completa. La exportación produce un archivo; no crea una conexión permanente con otra aplicación de hojas de cálculo.

Para una web o aplicación personalizada que vaya más allá de estas vistas, consulta [Programación](/es/docs/building/coding/). Crear esa aplicación es una tarea de desarrollo separada; no agrega automáticamente una vista integrada a la colección.
