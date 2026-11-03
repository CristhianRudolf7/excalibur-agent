import { create } from 'zustand'

export const googleQueryKey = ['google-status'] as const
export const googleStateKey = 'excalibur-google-oauth-state'
export type GoogleNotice = 'success' | 'error' | 'denied' | 'missing'
export const useGoogleNotice = create<{
  notice: GoogleNotice | null
  setNotice: (notice: GoogleNotice | null) => void
}>((set) => ({ notice: null, setNotice: (notice) => set({ notice }) }))

export function permissionName(scope: string): string {
  if (scope === 'https://mail.google.com/') return 'Gmail'
  if (scope === 'https://www.googleapis.com/auth/drive') return 'Drive'
  if (scope === 'https://www.googleapis.com/auth/documents') return 'Docs'
  if (scope === 'openid' || scope === 'email' || scope.endsWith('/userinfo.email'))
    return 'googleIdentity'
  return 'googlePermission'
}
