export function isViewerEnabled(value?: string) {
  return value !== '0' && value !== 'false'
}
export const viewerEnabled = isViewerEnabled(import.meta.env.EXCALIBUR_DOCUMENT_VIEWER)
