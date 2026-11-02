# Data-driven world

The world is a frontend-only visualization. It does not connect to the daemon,
gateway, paid services, or an external event source.

## Contracts and adapters

- `events.ts` defines producer-neutral events and runtime agent identities.
  Timestamps are epoch milliseconds. Text is an i18n key unless `literal` is true.
  Targets may identify a room, project, file, task, or deliverable. Agent lifecycle
  changes travel through the same event contract.
- `activity-map.ts` maps activities to room types, named spots, poses, status
  icons, dialog policy, and activity history policy. Pass an extended table to
  `WorldEngine` and `createWorldStore` to support a new activity. Unknown
  activities use the idle behavior.
- `layout.ts` defines configuration contracts without importing seed data.
  Rooms contain polygons, waypoint paths, named campus/interior spots,
  backgrounds, and deliverable destinations. Room/project targets take priority
  over activity room types. Rooms without background art use the configured
  fallback; the renderer can also draw a plain room when no asset exists.
- `store.ts` creates isolated sessions. It owns the FIFO dialog queue, bounded
  activity history, missions, identities, and active agents. It knows no producer.
- `chat-bridge.ts` is the application adapter for shared chat and task state.
  Existing chat replies are referenced without duplicating their history.

## Standalone engine

`engine/WorldEngine.ts` has no React or application store dependencies:

| Method                  | Responsibility                                                  |
| ----------------------- | --------------------------------------------------------------- |
| `mount(host)`           | Initialize renderer, listeners, visibility and ticker controls. |
| `unmount()`             | Stop timers and ticker; remove listeners and release textures.  |
| `setLayout(layout)`     | Render supplied rooms and replace the background.               |
| `upsertAgent(agent)`    | Add or update any identity, palette, or helper variant.         |
| `removeAgent(id)`       | Release a character and its sprite frames.                      |
| `applyEvent(event)`     | Resolve behavior and update routes, poses and lifecycle.        |
| `focusRoom(id)`         | Center a configured room, respecting drag and reduced motion.   |
| `onRoomClick(callback)` | Subscribe to room selection; returns an unsubscribe function.   |

`focusPoint` supports minimap navigation. The renderer interface can be replaced
independently; reuse tests use an injected recording renderer. Pixi uses a private
30 FPS ticker, nearest-neighbor textures and a pixel ratio capped at two. Idle
poses render on demand. Hidden/offscreen/reduced-motion worlds stop the ticker.

`WorldCanvas.tsx` is the React lifecycle adapter. Overlay positions update directly
through engine callbacks; per-frame state never enters React or Zustand.

## Reusable components

`PixelSprite` and `PixelPortrait` are shared pixel primitives; `PixelAvatar` adapts
existing shell identities to the same variants. `RpgDialog`, `DialogQueue`,
`ActivityFeed`, `MissionBoard`, `Minimap`, and the shell `ModeSwitch` accept data
and callbacks through documented props. They do not import seed data, a world
store, or an event producer. The activity feed can also be embedded in Chat.

## Seed producer

`seed/` contains six primary agents, four helper definitions, eight rooms, eight
missions, deliverables, fifteen history entries and an initial dialog. Three
helpers are initially active. The 52-event script covers every mapped activity,
spawns/removes helpers and loops every four minutes. `makeTimeline(seed)` and
`createSimulation(script, duration, epoch)` make it deterministic in tests.

`World` accepts an isolated `store` and a `producerFactory`. A future live adapter
can replace the producer without changing the engine, contracts, or components.
The default factory is `seed/producer.ts`.

Use `?view=world&worldDebug=1` to expose the diagnostic hook in a production
preview. Development and non-production modes expose it automatically.
`worldSpeed=4` accelerates development/test demos; it is ignored in production
unless `worldDebug=1` explicitly enables diagnostics. Both query values survive
room navigation and mode changes.

For deterministic browser tests, add `worldClock=manual` alongside diagnostics.
This disables automatic simulation, movement ticks and idle pose timers. After
`window.__excaliburWorld.ready` becomes true (renderer and scene textures loaded),
`advance(ms)` synchronously steps the event producer, agent movement and camera
interpolation in bounded increments, using the selected simulation speed.
`settle()` completes only camera interpolation without changing event time or
agent routes. The hook publishes `camera: { x, y, zoom }` and simulation `elapsed`
for assertions; teardown sets `ready: false` and removes the controls. Without
explicit diagnostics in production, the manual clock and hook are unavailable.
Browser tests use this clock and poll camera values with numerical tolerances;
visibility and reduced-motion tests still exercise their normal lifecycle rules.

## Camera controls and minimap

`Minimap.tsx` accepts the campus background plus colored room shapes, agent
markers and the real camera viewport. `minimap-transform.ts` defines one uniform
coordinate transform for all layers. `minimap-dom.ts` updates the live rectangle
and markers through the lifecycle adapter without per-frame React updates.
Clicking opens the full-map modal; dragging the viewport retains the grab offset and pans the camera. The header expand button and the compact map button in rooms/mobile open the same modal.

`WorldSettings.tsx` is a controlled vertical icon pill above the gear for cycling speed and changing zoom. The store persists
preferences safely; the engine applies the same speed to walking that the producer
uses for elapsed simulation time. Zoom steps are 75%, 100%, 125% and 150%; the cap keeps the authored background sharp.
Settings, wheel and two-finger pinch share the camera helpers and bounds. Pinch is
continuous during the gesture and snaps on release; one finger only pans.
Interiors support the same controls while retaining the saved campus position.

The diagnostic `running` flag covers active walks, camera transitions and poses
rendered on demand. `fps` is measured from delivered ticker frames or the actual
interval between on-demand pose frames. Hidden/offscreen/reduced-motion worlds
and Chat report `running: false`; no continuous ticker runs for idle poses.

Room signs, agent names and status icons counter-scale against the effective camera
scale, including interior cover scaling. Their on-screen size and native tooltips
stay constant; only the background and characters enlarge. Background textures
use resolution one, nearest filtering and no generated mipmaps.

## Full-map navigation

`MapModal.tsx` accepts the same background, shapes, markers and viewport as the
minimap. Shapes optionally include occupant portraits and active mission counts.
Room polygons and the room directory invoke the supplied room callback; empty
space invokes the world-coordinate navigation callback. The transform and
hit-testing live in `map-geometry.ts`, independent of the application store.

The native modal traps focus, dims the background and restores focus on close.
It supports Escape, its close button and backdrop clicks, with reduced-motion
alternatives to its scale/fade transition. On mobile it fills the screen and
places the room directory below the map. The deleted room chip list has no
replacement outside this modal.

Settings use a 38-pixel rounded column aligned with the gear, with icon buttons
for speed, zoom in and zoom out. `nextSpeed` cycles 0.5, 1, 2 and 4. Tooltips,
arrow-key navigation and the existing touch, persistence and zoom constraints
remain available in both campus and room views.
