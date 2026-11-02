export const whiteboardMessages = {
  whiteboard: ['Pizarra', 'Whiteboard'],
  boardLoading: ['Cargando pizarra…', 'Loading whiteboard…'],
  boardOffline: [
    'La pizarra no está disponible. Conecta el servidor e inténtalo de nuevo.',
    'Whiteboard unavailable. Connect the daemon and try again.',
  ],
  boardEmpty: [
    'El agente puede abrir una pizarra para explicar una idea. Tus pizarras aparecerán aquí.',
    'The agent can open a whiteboard to explain an idea. Your boards will appear here.',
  ],
  boardRetry: ['Actualizar pizarras', 'Refresh boards'],
  boardDrawing: ['Excalibur está dibujando…', 'Excalibur is drawing…'],
  boardPresence: ['Excalibur y tú', 'Excalibur and you'],
  boardFullscreen: ['Pantalla completa', 'Fullscreen'],
  boardPanel: ['Volver al panel', 'Return to panel'],
  boardClose: ['Cerrar pizarra', 'Close whiteboard'],
  boardExport: ['Exportar {{format}}', 'Export {{format}}'],
  boardSaving: ['Guardando…', 'Saving…'],
  boardSaved: ['Sincronizado', 'Synced'],
  boardError: [
    'No se pudo sincronizar. Tus cambios se conservan; reintenta.',
    'Could not sync. Your changes are preserved; retry.',
  ],
  boardList: ['Pizarras de la conversación', 'Conversation boards'],
} satisfies Record<string, [string, string]>
