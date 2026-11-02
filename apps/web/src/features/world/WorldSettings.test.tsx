import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { WorldSettings } from './WorldSettings'
import { clampZoom, snapZoom, stepZoom, zoomCamera } from './engine/camera'
import { initialWorldSettings, normalizeSpeed, saveWorldPreference, nextSpeed } from './preferences'
afterEach(() => {
  cleanup()
  localStorage.clear()
  vi.restoreAllMocks()
})
it('settings emits speed and zoom values, disables limits and closes with Escape or outside click', () => {
  const speed = vi.fn(),
    zoom = vi.fn(),
    view = render(<WorldSettings speed={1} zoom={0.75} onSpeed={speed} onZoom={zoom} />)
  const trigger = screen.getByRole('button', { name: 'Ajustes del mundo' })
  fireEvent.click(trigger)
  expect(screen.getByRole('button', { name: 'Reducir zoom' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: 'Aumentar zoom' }))
  expect(zoom).toHaveBeenCalledWith(1)
  fireEvent.click(screen.getByRole('button', { name: 'Aumentar velocidad' }))
  expect(speed).toHaveBeenCalledWith(2)
  view.rerender(<WorldSettings speed={4} zoom={1.5} onSpeed={speed} onZoom={zoom} />)
  expect(screen.getByRole('button', { name: 'Aumentar zoom' })).toBeDisabled()
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('toolbar')).toBeNull()
  expect(trigger).toHaveFocus()
  fireEvent.click(trigger)
  fireEvent.pointerDown(document.body)
  expect(screen.queryByRole('toolbar')).toBeNull()
})
it('zoom clamps, steps and keeps the visible center until map bounds require clamping', () => {
  expect(clampZoom(0.1)).toBe(0.75)
  expect(clampZoom(5)).toBe(1.5)
  expect(snapZoom(1.7)).toBe(1.5)
  expect(stepZoom(1.5, 1)).toBe(1.5)
  expect(stepZoom(2, 1)).toBe(1.5)
  expect(stepZoom(0.75, -1)).toBe(0.75)
  const after = zoomCamera(
    { x: 300, y: 200, zoom: 1 },
    2,
    { width: 1000, height: 800 },
    { width: 2400, height: 1600 },
  )
  expect(after).toEqual({ x: 550, y: 400, zoom: 2 })
  expect(after.x + 1000 / after.zoom / 2).toBe(800)
})
it('speed and zoom persist safely and reject invalid stored values', () => {
  saveWorldPreference('world-speed', 4)
  saveWorldPreference('world-zoom', 2)
  expect(initialWorldSettings()).toEqual({ speed: 4, zoom: 1.5 })
  localStorage.setItem('world-speed', 'invalid')
  localStorage.setItem('world-zoom', 'invalid')
  expect(initialWorldSettings()).toEqual({ speed: 1, zoom: 0.75 })
  expect(normalizeSpeed(0.5)).toBe(0.5)
  expect(normalizeSpeed(4)).toBe(4)
  expect(normalizeSpeed(NaN)).toBe(1)
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('Unavailable')
  })
  expect(() => saveWorldPreference('world-speed', 2)).not.toThrow()
})

it('speed cycle visits every preset and wraps back to the slowest speed', () => {
  expect([nextSpeed(0.5), nextSpeed(1), nextSpeed(2), nextSpeed(4)]).toEqual([1, 2, 4, 0.5])
  expect(nextSpeed(16)).toBe(0.5)
})
