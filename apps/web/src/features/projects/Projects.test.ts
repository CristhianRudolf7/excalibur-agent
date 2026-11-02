import { expect, it } from 'vitest'
import { validProjectPath } from './validation'
it('requires an absolute directory without traversal or NUL bytes', () => {
  expect(validProjectPath('/home/user/my project')).toBe(true)
  for (const path of ['', 'relative', '/home/../user', '/home/./user', '/tmp/\0bad'])
    expect(validProjectPath(path)).toBe(false)
})
