import i18n from '@/lib/i18n'
const entries = {
  extractionNotice: [
    'Some text could not be extracted. You can still read the original PDF.',
    'No se pudo extraer parte del texto. Puedes leer el PDF original.',
  ],
  opening: ['Opening file…', 'Abriendo archivo…'],
  openError: ['Could not open this file', 'No se pudo abrir el archivo'],
  openHelp: [
    'Check that the file is available in this workspace, then retry.',
    'Comprueba que el archivo está disponible en este espacio y reintenta.',
  ],
  unsupported: ['Preview unavailable', 'No se puede previsualizar'],
  unsupportedHelp: [
    'This file type cannot be previewed yet.',
    'Este tipo de archivo aún no admite vista previa.',
  ],
  title: ['Document viewer', 'Visor de documentos'],
  empty: ['Open a document or explore a sample.', 'Abre un documento o explora un ejemplo.'],
  loading: ['Loading document…', 'Cargando documento…'],
  error: ['Could not load document.', 'No se pudo cargar el documento.'],
  retry: ['Retry', 'Reintentar'],
  demo: ['Sample preview', 'Vista de ejemplo'],
  download: ['Download original', 'Descargar original'],
  close: ['Close document', 'Cerrar documento'],
  page: ['Page', 'Página'],
  section: ['Section', 'Sección'],
  slide: ['Slide', 'Diapositiva'],
  sheet: ['Sheet', 'Hoja'],
  previous: ['Previous', 'Anterior'],
  next: ['Next', 'Siguiente'],
  notes: ['Notes', 'Notas'],
  zoomIn: ['Zoom in', 'Ampliar'],
  zoomOut: ['Zoom out', 'Reducir'],
  fit: ['Fit width', 'Ajustar al ancho'],
  disabled: ['Document previews are disabled.', 'Las vistas de documentos están desactivadas.'],
  pdfError: [
    'Could not render PDF. Download the original or retry.',
    'No se pudo mostrar el PDF. Descarga el original o reintenta.',
  ],
  highlight: ['Cited passage', 'Pasaje citado'],
  noPages: ['No extracted content yet.', 'Aún no hay contenido extraído.'],
  pending: ['Pending', 'Pendiente'],
  processing: ['Processing', 'Procesando'],
  ready: ['Ready', 'Listo'],
  partial: ['Partial', 'Parcial'],
  failed: ['Failed', 'Error'],
  canceled: ['Canceled', 'Cancelado'],
} satisfies Record<string, [string, string]>
for (const [index, language] of ['en', 'es'].entries()) {
  i18n.addResourceBundle(
    language,
    'viewer',
    Object.fromEntries(Object.entries(entries).map(([key, value]) => [key, value[index]])),
  )
}
