export function validProjectPath(path: string) {
  return (
    path.startsWith('/') &&
    !path.includes('\0') &&
    !path.split('/').some((part) => part === '.' || part === '..')
  )
}
